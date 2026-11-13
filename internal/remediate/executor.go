// Package remediate plans and, only when policy allows, performs tier-2 runbooks.
package remediate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/larryjoelane/healbot/internal/audit"
	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/platform"
	"github.com/larryjoelane/healbot/internal/safety"
)

// Executor is the only component allowed to call mutating runtime methods.
type Executor struct {
	RT     platform.Runtime
	Gate   *safety.Gate
	Audit  *audit.Log
	Now    func() time.Time
	HTTP   *http.Client
	Limits func(config.Resource) safety.Limits
	seq    atomic.Uint64
}

func (e *Executor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Executor) limits(res config.Resource) safety.Limits {
	if e.Limits != nil {
		return e.Limits(res)
	}
	return safety.Limits{Cooldown: 10 * time.Minute, MaxPerHour: 3, BreakerFailures: 2, BreakerOpenFor: 30 * time.Minute}
}

// Run either records a dry-run plan or performs one allowlisted action and
// checks health afterwards. It never invents an action outside ActionKind.
func (e *Executor) Run(ctx context.Context, inc model.Incident, res config.Resource, kind model.ActionKind, dry bool) model.ActionRecord {
	now := e.now()
	rec := model.ActionRecord{
		ID:        fmt.Sprintf("act_%d", e.seq.Add(1)),
		Kind:      kind,
		DryRun:    dry,
		StartedAt: now,
	}
	plan := describe(inc, res, kind)
	finish := func(outcome, detail, auditKind string) model.ActionRecord {
		rec.Outcome = outcome
		rec.Detail = detail
		rec.FinishedAt = e.now()
		if e.Audit != nil {
			e.Audit.RecordSync(model.AuditEvent{
				Time: rec.FinishedAt, IncidentID: inc.ID, Kind: auditKind,
				Message: detail,
				Fields: map[string]string{
					"action": string(kind), "outcome": outcome, "resource": inc.ResourceID, "dry_run": fmt.Sprintf("%t", dry),
				},
			})
		}
		return rec
	}
	if !kind.Valid() {
		return finish("skipped", "action is not in the runbook catalog", model.AuditRemediationBlocked)
	}
	if e.Gate != nil && e.Gate.Killed() {
		return finish("killed", "kill switch is engaged; "+plan, model.AuditRemediationBlocked)
	}
	if dry {
		return finish("dry_run", "dry-run: "+plan, model.AuditRemediationDryRun)
	}
	if e.Gate != nil {
		permit := e.Gate.Admit(inc.ResourceID, now, e.limits(res))
		if !permit.Allowed {
			return finish(permit.Reason, plan+" blocked: "+permit.Reason, model.AuditRemediationBlocked)
		}
	}
	if err := e.execute(ctx, inc, res, kind); err != nil {
		if e.Gate != nil {
			e.Gate.Record(inc.ResourceID, e.now(), false, e.limits(res))
		}
		outcome := "failed"
		if errors.Is(err, platform.ErrAtCapacity) {
			outcome = "at_capacity"
		}
		return finish(outcome, plan+" failed: "+err.Error(), model.AuditRemediationExecuted)
	}
	status, err := e.verify(ctx, res, targetID(inc, res, kind))
	if e.Gate != nil {
		e.Gate.Record(inc.ResourceID, e.now(), err == nil && status.Healthy, e.limits(res))
	}
	if err != nil {
		return finish("unverified", plan+" applied but health could not be confirmed: "+err.Error(), model.AuditVerification)
	}
	if !status.Healthy {
		return finish("not_hold", plan+" applied; health did not hold: "+status.Detail, model.AuditVerification)
	}
	return finish("verified", plan+" applied; health holds: "+status.Detail, model.AuditVerification)
}

func (e *Executor) execute(ctx context.Context, inc model.Incident, res config.Resource, kind model.ActionKind) error {
	if e.RT == nil {
		return errors.New("no runtime configured")
	}
	switch kind {
	case model.ActionRestart:
		return e.RT.Restart(ctx, inc.ResourceID)
	case model.ActionRestartConsumer:
		return e.RT.Restart(ctx, targetID(inc, res, kind))
	case model.ActionSlotSwap:
		slot := res.KnownGoodSlot
		if slot == "" {
			slot = "staging"
		}
		return e.RT.SwapSlot(ctx, inc.ResourceID, slot)
	case model.ActionScaleOut:
		max := res.MaxInstances
		if max < 1 {
			max = 1
		}
		return e.RT.ScaleOut(ctx, inc.ResourceID, 1, int32(max))
	case model.ActionRestartRevision:
		return e.RT.RestartRevision(ctx, inc.ResourceID, res.KnownGoodRevision)
	case model.ActionRollbackRevision:
		return e.RT.RollbackRevision(ctx, inc.ResourceID, res.KnownGoodRevision)
	default:
		return fmt.Errorf("unsupported action %s", kind)
	}
}

func (e *Executor) verify(ctx context.Context, res config.Resource, resourceID string) (model.HealthStatus, error) {
	if res.HealthURL != "" {
		return e.probe(ctx, res.HealthURL)
	}
	if e.RT == nil {
		return model.HealthStatus{}, errors.New("no health probe configured")
	}
	return e.RT.Health(ctx, resourceID)
}

func (e *Executor) probe(ctx context.Context, url string) (model.HealthStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return model.HealthStatus{}, err
	}
	req.Header.Set("User-Agent", "healbot/0.1")
	client := e.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return model.HealthStatus{}, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return model.HealthStatus{Healthy: true, Detail: res.Status}, nil
	}
	return model.HealthStatus{Healthy: false, Detail: res.Status}, nil
}

func targetID(inc model.Incident, res config.Resource, kind model.ActionKind) string {
	if kind == model.ActionRestartConsumer && res.ConsumerResourceID != "" {
		return res.ConsumerResourceID
	}
	return inc.ResourceID
}

func describe(inc model.Incident, res config.Resource, kind model.ActionKind) string {
	name := inc.ResourceName
	if name == "" {
		name = inc.ResourceID
	}
	switch kind {
	case model.ActionSlotSwap:
		slot := res.KnownGoodSlot
		if slot == "" {
			slot = "staging"
		}
		return fmt.Sprintf("swap slot %s with production on %s", slot, name)
	case model.ActionScaleOut:
		return fmt.Sprintf("scale out %s by 1 instance, ceiling %d", name, res.MaxInstances)
	case model.ActionRestart:
		return fmt.Sprintf("restart %s", name)
	case model.ActionRestartConsumer:
		return fmt.Sprintf("restart queue consumer %s; messages are not purged", targetID(inc, res, kind))
	case model.ActionRestartRevision:
		return fmt.Sprintf("restart the active container revision of %s", name)
	case model.ActionRollbackRevision:
		rev := res.KnownGoodRevision
		if rev == "" {
			rev = "the previous revision"
		}
		return fmt.Sprintf("send traffic on %s back to %s", name, rev)
	default:
		return string(kind)
	}
}
