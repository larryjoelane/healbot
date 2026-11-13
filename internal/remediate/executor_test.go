package remediate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/larryjoelane/healbot/internal/audit"
	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/platform/fake"
	"github.com/larryjoelane/healbot/internal/safety"
)

func TestSelectSlotSwap(t *testing.T) {
	kind, ok := Select(model.Incident{
		ResourceType: model.TypeWebSite,
		Hypothesis: &model.Hypothesis{
			Category: model.CategoryCode, LikelyCode: true, SuggestedAction: model.ActionSlotSwap,
		},
	}, []string{"restart", "slot_swap", "scale_out"})
	if !ok || kind != model.ActionSlotSwap {
		t.Fatalf("%s %v", kind, ok)
	}
}

func TestSelectRefusesContainerSlotSwap(t *testing.T) {
	_, ok := Select(model.Incident{
		ResourceType: model.TypeContainerApp,
		Hypothesis:   &model.Hypothesis{SuggestedAction: model.ActionSlotSwap, Category: model.CategoryCode},
	}, []string{"slot_swap", "rollback_revision"})
	if !ok {
		t.Fatal("expected rollback fallback")
	}
}

func TestDryRunDoesNotCallRuntime(t *testing.T) {
	rt := fake.New(time.Now)
	log, err := audit.New(t.TempDir()+"/audit.jsonl", 100, true)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	ex := &Executor{RT: rt, Gate: safety.New(false), Audit: log}
	rec := ex.Run(context.Background(), model.Incident{
		ID: "inc_1", ResourceID: "rid", ResourceName: "example-api", ResourceType: model.TypeWebSite,
	}, config.Resource{KnownGoodSlot: "staging", MaxInstances: 3}, model.ActionSlotSwap, true)
	if rec.Outcome != "dry_run" {
		t.Fatal(rec.Outcome)
	}
	if len(rt.Snapshot()) != 0 {
		t.Fatalf("dry-run mutated: %+v", rt.Snapshot())
	}
	if len(log.List(0)) != 1 || log.List(0)[0].Kind != model.AuditRemediationDryRun {
		t.Fatalf("%+v", log.List(0))
	}
}

func TestLiveNotHoldTripsBreaker(t *testing.T) {
	rt := fake.New(time.Now)
	gate := safety.New(false)
	ex := &Executor{
		RT:   rt,
		Gate: gate,
		Now:  time.Now,
		Limits: func(config.Resource) safety.Limits {
			return safety.Limits{MaxPerHour: 10, BreakerFailures: 2, BreakerOpenFor: time.Hour}
		},
	}
	res := config.Resource{MaxInstances: 3, MaxActionsPerHour: 5}
	inc := model.Incident{ID: "inc", ResourceID: "rid", ResourceName: "api", ResourceType: model.TypeWebSite}
	rec := ex.Run(context.Background(), inc, res, model.ActionRestart, false)
	if rec.Outcome != "not_hold" {
		t.Fatal(rec.Outcome, rec.Detail)
	}
	if len(rt.Snapshot()) != 1 || rt.Snapshot()[0].Op != "restart" {
		t.Fatal(rt.Snapshot())
	}
	rec = ex.Run(context.Background(), inc, res, model.ActionRestart, false)
	if rec.Outcome != "not_hold" {
		t.Fatal(rec)
	}
	rec = ex.Run(context.Background(), inc, res, model.ActionRestart, false)
	if rec.Outcome != "breaker_open" {
		t.Fatalf("%+v", rec)
	}
}

func TestHealthURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	rt := fake.New(time.Now)
	ex := &Executor{RT: rt, Gate: safety.New(false), HTTP: srv.Client()}
	rec := ex.Run(context.Background(), model.Incident{ID: "i", ResourceID: "r", ResourceName: "api"}, config.Resource{HealthURL: srv.URL, MaxInstances: 2, MaxActionsPerHour: 5}, model.ActionRestart, false)
	if rec.Outcome != "verified" {
		t.Fatal(rec.Outcome, rec.Detail)
	}
}

func TestKillSwitchBlocksDryRun(t *testing.T) {
	rt := fake.New(nil)
	ex := &Executor{RT: rt, Gate: safety.New(true)}
	rec := ex.Run(context.Background(), model.Incident{ID: "i", ResourceName: "api"}, config.Resource{}, model.ActionRestart, true)
	if rec.Outcome != "killed" {
		t.Fatal(rec.Outcome)
	}
	if len(rt.Snapshot()) != 0 {
		t.Fatal(rt.Snapshot())
	}
}
