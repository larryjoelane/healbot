// Package policy decides which tier is allowed to run.
package policy

import (
	"strings"

	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/model"
)

// Engine evaluates escalation rules against a resource policy.
type Engine struct {
	cfg     config.Config
	counts  map[int]struct{}
	indexed map[string]config.Resource
}

// New builds an engine from config.
func New(cfg config.Config) *Engine {
	e := &Engine{
		cfg:     cfg,
		counts:  map[int]struct{}{},
		indexed: map[string]config.Resource{},
	}
	for _, n := range cfg.Escalation.NotifyCounts {
		e.counts[n] = struct{}{}
	}
	for _, r := range cfg.Resources {
		e.indexed[strings.ToLower(r.ID)] = r
	}
	return e
}

// Resource returns the policy for an Azure resource. Unknown resources are
// diagnose-only: max tier 1, dry-run, no actions.
func (e *Engine) Resource(id string) config.Resource {
	if r, ok := e.indexed[strings.ToLower(id)]; ok {
		return r
	}
	for _, r := range e.cfg.Resources {
		if strings.HasSuffix(r.ID, "*") && strings.HasPrefix(strings.ToLower(id), strings.ToLower(strings.TrimSuffix(r.ID, "*"))) {
			return r
		}
	}
	return config.Resource{ID: id, MaxTier: 1, MaxInstances: 1}
}

// ShouldNotify reports whether this observation should page a human channel.
func (e *Engine) ShouldNotify(created, severityRaised bool, count int) bool {
	if severityRaised {
		return true
	}
	_, ok := e.counts[count]
	return ok || (created && count == 1)
}

// Want reports whether a tier's rules match. tier2Outcome is ignored for tiers
// other than 3. An empty outcome means tier 2 has not finished yet.
func (e *Engine) Want(tier int, inc model.Incident, recentDeploy bool, tier2Outcome string) (bool, []string) {
	var matched []string
	for _, rule := range e.cfg.Escalation.Rules {
		if rule.Tier != tier {
			continue
		}
		if !match(rule, inc, recentDeploy, tier2Outcome) {
			continue
		}
		matched = append(matched, rule.Name)
	}
	return len(matched) > 0, matched
}

func match(rule config.Rule, inc model.Incident, recent bool, outcome string) bool {
	minSev := model.Sev4
	if rule.MinSeverity != "" {
		if parsed, ok := model.ParseSeverity(rule.MinSeverity); ok {
			minSev = parsed
		}
	}
	if !inc.Severity.Worse(minSev) {
		return false
	}
	need := rule.MinCount
	if need < 1 {
		need = 1
	}
	if inc.Count < need {
		return false
	}
	if rule.MinErrorRate > 0 && inc.ErrorRate < rule.MinErrorRate {
		return false
	}
	if rule.RequiresRecentDeploy && !recent {
		return false
	}
	if rule.RequiresLikelyCode {
		if inc.Hypothesis == nil || !inc.Hypothesis.LikelyCode {
			return false
		}
	}
	if len(rule.Categories) > 0 {
		cat := model.CategoryUnknown
		if inc.Hypothesis != nil {
			cat = inc.Hypothesis.Category
		}
		ok := false
		for _, c := range rule.Categories {
			if strings.EqualFold(c, string(cat)) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if rule.Tier == 3 && len(rule.WhenTier2) > 0 {
		ok := false
		for _, w := range rule.WhenTier2 {
			if w == outcome {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
