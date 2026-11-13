package policy

import (
	"testing"

	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/model"
)

func TestUnknownResourceIsDiagnoseOnly(t *testing.T) {
	e := New(config.Demo())
	res := e.Resource("/subscriptions/s/resourceGroups/rg/providers/Microsoft.Web/sites/other")
	if res.MaxTier != 1 {
		t.Fatal(res.MaxTier)
	}
	known := e.Resource(config.DemoResourceID)
	if known.MaxTier != 3 {
		t.Fatal(known.MaxTier)
	}
}

func TestTierRules(t *testing.T) {
	e := New(config.Demo())
	inc := model.Incident{
		Severity: model.Sev2,
		Count:    1,
		Hypothesis: &model.Hypothesis{
			Category:   model.CategoryCode,
			LikelyCode: true,
		},
	}
	if ok, _ := e.Want(2, inc, false, ""); ok {
		t.Fatal("code incident without a recent deploy must not enter tier 2")
	}
	if ok, names := e.Want(2, inc, true, ""); !ok || names[0] != "rollback-after-deploy" {
		t.Fatalf("%v %v", ok, names)
	}
	if ok, _ := e.Want(3, inc, true, ""); ok {
		t.Fatal("tier 3 waits for a tier 2 outcome")
	}
	if ok, _ := e.Want(3, inc, true, "dry_run"); !ok {
		t.Fatal("dry-run should escalate to a pull request")
	}
	inc.Hypothesis.LikelyCode = false
	inc.Hypothesis.Category = model.CategoryCapacity
	if ok, _ := e.Want(2, inc, false, ""); !ok {
		t.Fatal("capacity should be allowed without a deploy")
	}
	if ok, _ := e.Want(3, inc, false, "dry_run"); ok {
		t.Fatal("capacity must not open a code pull request")
	}
}

func TestNotifyMilestones(t *testing.T) {
	e := New(config.Demo())
	if !e.ShouldNotify(true, false, 1) {
		t.Fatal("first sighting notifies")
	}
	if e.ShouldNotify(false, false, 2) {
		t.Fatal("a quiet repeat should not notify")
	}
	if !e.ShouldNotify(false, true, 2) {
		t.Fatal("severity increase notifies")
	}
	if !e.ShouldNotify(false, false, 10) {
		t.Fatal("milestone notifies")
	}
}
