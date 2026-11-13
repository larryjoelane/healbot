package remediate

import "github.com/larryjoelane/healbot/internal/model"

// Select picks the safest allowlisted action that fits the hypothesis.
// It returns false when nothing in the catalog is both allowed and relevant.
func Select(inc model.Incident, allowed []string) (model.ActionKind, bool) {
	set := map[model.ActionKind]struct{}{}
	for _, raw := range allowed {
		kind := model.ActionKind(raw)
		if kind.Valid() && compatible(kind, inc.ResourceType) {
			set[kind] = struct{}{}
		}
	}
	if inc.Hypothesis != nil {
		if _, ok := set[inc.Hypothesis.SuggestedAction]; ok {
			return inc.Hypothesis.SuggestedAction, true
		}
	}
	cat := model.CategoryUnknown
	if inc.Hypothesis != nil {
		cat = inc.Hypothesis.Category
	}
	for _, kind := range []model.ActionKind{
		model.ActionSlotSwap,
		model.ActionRollbackRevision,
		model.ActionRestart,
		model.ActionRestartRevision,
		model.ActionRestartConsumer,
		model.ActionScaleOut,
	} {
		if _, ok := set[kind]; ok && categoryAllows(cat, kind) {
			return kind, true
		}
	}
	return "", false
}

func compatible(kind model.ActionKind, resourceType string) bool {
	switch kind {
	case model.ActionSlotSwap, model.ActionRestart, model.ActionRestartConsumer:
		return resourceType != model.TypeContainerApp
	case model.ActionRestartRevision, model.ActionRollbackRevision:
		return resourceType == model.TypeContainerApp
	default:
		return true
	}
}

func categoryAllows(cat model.Category, kind model.ActionKind) bool {
	switch cat {
	case model.CategoryCode, model.CategoryDeployment, model.CategoryConfig:
		return kind == model.ActionSlotSwap || kind == model.ActionRollbackRevision
	case model.CategoryCapacity:
		return kind == model.ActionScaleOut
	case model.CategoryDependency:
		return kind == model.ActionRestart || kind == model.ActionRestartRevision || kind == model.ActionRestartConsumer
	default:
		return false
	}
}
