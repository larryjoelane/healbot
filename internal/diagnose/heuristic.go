// Package diagnose produces a root-cause hypothesis.
package diagnose

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/platform"
)

// Diagnoser explains an incident. Implementations must be safe to call when
// the model provider is down; the heuristic always is.
type Diagnoser interface {
	Diagnose(ctx context.Context, inc model.Incident, bundle model.ContextBundle) (model.Hypothesis, error)
}

// Gatherer loads read-only context from the cloud, or from the fake runtime.
type Gatherer struct {
	RT     platform.Runtime
	Window time.Duration
	Now    func() time.Time
}

// Gather lists recent deployments and marks whether one landed inside the window.
func (g Gatherer) Gather(ctx context.Context, inc model.Incident) (model.ContextBundle, error) {
	if g.RT == nil {
		return model.ContextBundle{}, nil
	}
	now := time.Now()
	if g.Now != nil {
		now = g.Now()
	}
	window := g.Window
	if window <= 0 {
		window = 2 * time.Hour
	}
	deps, err := g.RT.ListChanges(ctx, inc.ResourceID)
	bundle := model.ContextBundle{Deployments: deps}
	for _, d := range deps {
		if d.At.IsZero() {
			continue
		}
		age := now.Sub(d.At)
		if age >= 0 && age <= window {
			bundle.RecentDeploy = true
		}
	}
	return bundle, err
}

// Heuristic is the default diagnoser. It does not call a model.
type Heuristic struct{}

// Diagnose classifies the incident from the exception, message, and deploy context.
func (Heuristic) Diagnose(_ context.Context, inc model.Incident, bundle model.ContextBundle) (model.Hypothesis, error) {
	text := strings.ToLower(inc.ExceptionType + "\n" + inc.Message + "\n" + inc.StackTrace + "\n" + inc.Operation)
	h := model.Hypothesis{
		Provider:   "heuristic",
		Category:   model.CategoryUnknown,
		Confidence: 0.45,
		Summary:    "The signal does not match a known failure shape. A human should look at the stack and the last deployment.",
	}
	switch {
	case strings.Contains(inc.ExceptionType, "NullReference") || strings.Contains(text, "object reference not set") || strings.Contains(text, "nullreference"):
		h.Category = model.CategoryCode
		h.LikelyCode = true
		h.Confidence = 0.86
		h.Summary = fmt.Sprintf("%s in %s looks like a null dereference introduced with the running build.", nonEmpty(inc.ExceptionType, "NullReferenceException"), nonEmpty(inc.TopFrame, "the top frame"))
		if bundle.RecentDeploy {
			h.SuggestedAction = suggestRollback(inc.ResourceType)
			h.Confidence = 0.9
		}
	case strings.Contains(text, "argumentnullexception") || strings.Contains(text, "argument null"):
		h.Category = model.CategoryCode
		h.LikelyCode = true
		h.Confidence = 0.8
		h.Summary = "An argument-null failure is reaching a boundary that used to reject it earlier."
		if bundle.RecentDeploy {
			h.SuggestedAction = suggestRollback(inc.ResourceType)
		}
	case strings.Contains(text, "cpu") || strings.Contains(text, "memory") || strings.Contains(text, "saturation") || strings.Contains(text, "outofmemory"):
		h.Category = model.CategoryCapacity
		h.Confidence = 0.78
		h.Summary = "The resource looks capacity constrained. Scaling out is the reversible first move."
		h.SuggestedAction = model.ActionScaleOut
	case strings.Contains(text, "timeout") || strings.Contains(text, "502") || strings.Contains(text, "503") || strings.Contains(text, "dependency"):
		h.Category = model.CategoryDependency
		h.Confidence = 0.7
		h.Summary = "Calls to a dependency are failing. Restarting the caller drops stuck connections without changing code."
		if inc.ResourceType == model.TypeContainerApp {
			h.SuggestedAction = model.ActionRestartRevision
		} else {
			h.SuggestedAction = model.ActionRestart
		}
	case strings.Contains(text, "poison") || strings.Contains(text, "message lock") || strings.Contains(text, "queue"):
		h.Category = model.CategoryDependency
		h.Confidence = 0.72
		h.Summary = "A queue consumer looks stuck. Healbot will restart the consumer process. It will not purge or dead-letter the queue."
		h.SuggestedAction = model.ActionRestartConsumer
	case strings.Contains(text, "config") || strings.Contains(text, "appsetting") || strings.Contains(text, "missing key"):
		h.Category = model.CategoryConfig
		h.LikelyCode = false
		h.Confidence = 0.64
		h.Summary = "A configuration value looks missing or wrong. Rolling back the last slot swap restores the previous settings."
		if bundle.RecentDeploy {
			h.SuggestedAction = suggestRollback(inc.ResourceType)
		}
	}
	if inc.ErrorRate > 0 {
		h.Evidence = append(h.Evidence, fmt.Sprintf("error rate %.1f%%", inc.ErrorRate*100))
	}
	if inc.ExceptionType != "" {
		h.Evidence = append(h.Evidence, "exception "+inc.ExceptionType)
	}
	if inc.TopFrame != "" {
		h.Evidence = append(h.Evidence, "top frame "+inc.TopFrame)
	}
	if inc.Operation != "" {
		h.Evidence = append(h.Evidence, "operation "+inc.Operation)
	}
	for _, d := range bundle.Deployments {
		if d.Active {
			h.Evidence = append(h.Evidence, fmt.Sprintf("active deployment %s at %s", nonEmpty(d.Name, d.Revision), d.At.UTC().Format(time.RFC3339)))
		}
	}
	if bundle.RecentDeploy {
		h.Evidence = append(h.Evidence, "a deployment landed inside the escalation window")
	}
	if h.Confidence > 0.95 {
		h.Confidence = 0.95
	}
	return h, nil
}

func suggestRollback(resourceType string) model.ActionKind {
	if resourceType == model.TypeContainerApp {
		return model.ActionRollbackRevision
	}
	return model.ActionSlotSwap
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
