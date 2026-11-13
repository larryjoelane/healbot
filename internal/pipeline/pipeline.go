// Package pipeline walks one signal through grouping, diagnosis, tier-2
// remediation, and a tier-3 pull request.
package pipeline

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/larryjoelane/healbot/internal/audit"
	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/diagnose"
	"github.com/larryjoelane/healbot/internal/fix"
	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/notify"
	"github.com/larryjoelane/healbot/internal/policy"
	"github.com/larryjoelane/healbot/internal/remediate"
	"github.com/larryjoelane/healbot/internal/safety"
	"github.com/larryjoelane/healbot/internal/store"
)

// Pipeline is the tiered decision loop.
type Pipeline struct {
	Store    *store.Store
	Audit    *audit.Log
	Gate     *safety.Gate
	Policy   *policy.Engine
	Gather   diagnose.Gatherer
	Diagnose diagnose.Diagnoser
	Exec     *remediate.Executor
	Fix      fix.Service
	Notify   []notify.Delivery
	DryRun   func(config.Resource) bool
	Now      func() time.Time
	seq      atomic.Uint64
}

// Result is the incident after this signal was applied.
type Result struct {
	Incident model.Incident `json:"incident"`
	Created  bool           `json:"created"`
	Matched  bool           `json:"matched"`
}

func (p *Pipeline) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// HandleSignal groups the signal and, for a new or worsened incident, runs the tiers.
func (p *Pipeline) HandleSignal(ctx context.Context, sig *model.Signal) (Result, error) {
	if sig.ID == "" {
		sig.ID = fmt.Sprintf("sig_%d", p.seq.Add(1))
	}
	if sig.ReceivedAt.IsZero() {
		sig.ReceivedAt = p.now()
	}
	if sig.Resolved {
		inc, ok := p.Store.ResolveOpen(sig)
		p.event(model.AuditIncidentResolved, inc.ID, "monitor condition resolved", map[string]string{"matched": fmt.Sprintf("%t", ok)})
		return Result{Incident: inc, Matched: ok}, nil
	}
	abs := p.Store.Absorb(sig)
	kind := model.AuditIncidentGrouped
	msg := "signal grouped into an open incident"
	if abs.Created {
		kind = model.AuditIncidentOpened
		msg = "incident opened"
	}
	p.event(model.AuditSignalReceived, abs.Incident.ID, sig.Message, map[string]string{
		"source": string(sig.Source), "resource": sig.ResourceID, "severity": sig.Severity.String(),
	})
	p.event(kind, abs.Incident.ID, msg, map[string]string{"count": fmt.Sprintf("%d", abs.Incident.Count)})

	inc := abs.Incident
	if inc.Suppressed {
		p.event(model.AuditSuppressed, inc.ID, "automation skipped; incident is suppressed", nil)
		return Result{Incident: inc, Created: abs.Created, Matched: true}, nil
	}
	automate := abs.Created || abs.SeverityRaised || p.milestone(inc.Count)
	if !automate {
		return Result{Incident: inc, Created: false, Matched: true}, nil
	}

	bundle, gerr := p.Gather.Gather(ctx, inc)
	if gerr != nil {
		p.event(model.AuditDiagnosisCompleted, inc.ID, "context gather failed: "+gerr.Error(), nil)
	}
	diag := p.Diagnose
	if diag == nil {
		diag = diagnose.Heuristic{}
	}
	hyp, err := diag.Diagnose(ctx, inc, bundle)
	if err != nil {
		hyp = model.Hypothesis{Summary: "diagnosis failed: " + err.Error(), Category: model.CategoryUnknown, Provider: "error"}
	}
	inc, _ = p.Store.Update(inc.ID, func(x *model.Incident) {
		x.Hypothesis = &hyp
		x.Status = model.StatusDiagnosed
		if x.TierReached < model.TierDetect {
			x.TierReached = model.TierDetect
		}
	})
	p.event(model.AuditDiagnosisCompleted, inc.ID, hyp.Summary, map[string]string{
		"category": string(hyp.Category), "provider": hyp.Provider, "likely_code": fmt.Sprintf("%t", hyp.LikelyCode),
		"confidence": fmt.Sprintf("%.2f", hyp.Confidence),
	})

	resPol := p.Policy.Resource(inc.ResourceID)
	if p.Policy.ShouldNotify(abs.Created, abs.SeverityRaised, inc.Count) {
		p.notify(ctx, inc, "diagnosed")
	}
	if p.Gate != nil && p.Gate.Killed() {
		inc, _ = p.Store.Update(inc.ID, func(x *model.Incident) {
			x.Status = model.StatusAwaitingHuman
		})
		p.event(model.AuditAutomationBlocked, inc.ID, "kill switch is engaged; tier 2 and tier 3 were not run", nil)
		return Result{Incident: inc, Created: abs.Created, Matched: true}, nil
	}

	outcome := "skipped"
	want2, rules2 := p.Policy.Want(2, inc, bundle.RecentDeploy, "")
	dry := true
	if p.DryRun != nil {
		dry = p.DryRun(resPol)
	}
	p.event(model.AuditPolicyDecision, inc.ID, "tier policy evaluated", map[string]string{
		"max_tier":   fmt.Sprintf("%d", resPol.MaxTier),
		"want_tier2": fmt.Sprintf("%t", want2 && resPol.MaxTier >= 2),
		"dry_run":    fmt.Sprintf("%t", dry),
		"rules":      join(rules2),
	})
	if want2 && resPol.MaxTier >= 2 && p.Exec != nil {
		kind, ok := remediate.Select(inc, resPol.AllowedActions)
		if !ok {
			p.event(model.AuditRemediationBlocked, inc.ID, "no allowlisted runbook fits this hypothesis", nil)
		} else {
			rec := p.Exec.Run(ctx, inc, resPol, kind, dry)
			inc, _ = p.Store.Update(inc.ID, func(x *model.Incident) {
				x.Actions = append(x.Actions, rec)
				if x.TierReached < model.TierRemediate {
					x.TierReached = model.TierRemediate
				}
				x.Status = model.StatusRemediating
			})
			outcome = rec.Outcome
			if rec.Outcome == "verified" {
				inc, _ = p.Store.Update(inc.ID, func(x *model.Incident) { x.Status = model.StatusResolved })
				p.notify(ctx, inc, "resolved")
				return Result{Incident: inc, Created: abs.Created, Matched: true}, nil
			}
		}
	}

	want3, rules3 := p.Policy.Want(3, inc, bundle.RecentDeploy, outcome)
	if want3 && resPol.MaxTier >= 3 && inc.PullRequest == nil {
		ref, files, err := p.Fix.Open(ctx, inc, outcome)
		if err != nil {
			inc, _ = p.Store.Update(inc.ID, func(x *model.Incident) { x.Status = model.StatusAwaitingHuman })
			p.event(model.AuditPRFailed, inc.ID, err.Error(), map[string]string{"rules": join(rules3)})
			return Result{Incident: inc, Created: abs.Created, Matched: true}, nil
		}
		inc, _ = p.Store.Update(inc.ID, func(x *model.Incident) {
			x.PullRequest = &ref
			x.ProposedFiles = files
			x.Status = model.StatusPROpened
			x.TierReached = model.TierCodeFix
		})
		p.event(model.AuditPROpened, inc.ID, ref.Title, map[string]string{
			"provider": ref.Provider, "url": ref.URL, "branch": ref.Branch, "rules": join(rules3),
		})
		p.event(model.AuditEscalated, inc.ID, "tier 2 outcome "+outcome+" escalated to a pull request for human review", nil)
		p.notify(ctx, inc, "pr_opened")
		return Result{Incident: inc, Created: abs.Created, Matched: true}, nil
	}

	inc, _ = p.Store.Update(inc.ID, func(x *model.Incident) {
		if x.Status != model.StatusPROpened && x.Status != model.StatusResolved {
			if outcome == "skipped" && x.TierReached <= model.TierDetect {
				x.Status = model.StatusDiagnosed
			} else {
				x.Status = model.StatusAwaitingHuman
			}
		}
	})
	return Result{Incident: inc, Created: abs.Created, Matched: true}, nil
}

func (p *Pipeline) milestone(count int) bool {
	if p.Policy == nil || count <= 1 {
		return false
	}
	return p.Policy.ShouldNotify(false, false, count)
}

func (p *Pipeline) notify(ctx context.Context, inc model.Incident, kind string) {
	msg := notify.FromIncident(inc, kind)
	for _, d := range p.Notify {
		if d == nil {
			continue
		}
		if err := d.Send(ctx, msg); err != nil {
			p.event(model.AuditNotifyFailed, inc.ID, d.Name()+": "+err.Error(), nil)
			continue
		}
		p.event(model.AuditNotifySent, inc.ID, "notified "+d.Name(), map[string]string{"channel": d.Name(), "kind": kind})
	}
}

func (p *Pipeline) event(kind, incidentID, message string, fields map[string]string) {
	if p.Audit == nil {
		return
	}
	p.Audit.RecordSync(model.AuditEvent{
		Time: p.now(), IncidentID: incidentID, Kind: kind, Message: message, Fields: fields,
	})
}

func join(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += "," + p
	}
	return out
}
