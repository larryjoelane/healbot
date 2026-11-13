package pipeline

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/larryjoelane/healbot/internal/audit"
	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/diagnose"
	"github.com/larryjoelane/healbot/internal/fix"
	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/notify"
	"github.com/larryjoelane/healbot/internal/platform/fake"
	"github.com/larryjoelane/healbot/internal/policy"
	"github.com/larryjoelane/healbot/internal/remediate"
	"github.com/larryjoelane/healbot/internal/safety"
	"github.com/larryjoelane/healbot/internal/store"
)

func TestDemoFlowTier1DryRunTier3(t *testing.T) {
	p, rt, mem, log := testPipe(t, false)
	sig := exampleSignal()
	res, err := p.HandleSignal(context.Background(), sig)
	if err != nil {
		t.Fatal(err)
	}
	inc := res.Incident
	if inc.Status != model.StatusPROpened || inc.TierReached != model.TierCodeFix {
		t.Fatalf("status=%s tier=%d", inc.Status, inc.TierReached)
	}
	if inc.Hypothesis == nil || !inc.Hypothesis.LikelyCode || inc.Hypothesis.SuggestedAction != model.ActionSlotSwap {
		t.Fatalf("%+v", inc.Hypothesis)
	}
	if len(inc.Actions) != 1 || inc.Actions[0].Outcome != "dry_run" || inc.Actions[0].Kind != model.ActionSlotSwap {
		t.Fatalf("%+v", inc.Actions)
	}
	if calls := rt.Snapshot(); len(calls) != 0 {
		t.Fatalf("dry-run called azure: %+v", calls)
	}
	if inc.PullRequest == nil || inc.PullRequest.Provider != "mock" {
		t.Fatal(inc.PullRequest)
	}
	pr, ok := mem.Get(inc.PullRequest.ID)
	if !ok || len(pr.Files) < 2 {
		t.Fatalf("%+v", pr)
	}
	kinds := map[string]bool{}
	for _, e := range log.ByIncident(inc.ID) {
		kinds[e.Kind] = true
	}
	for _, need := range []string{model.AuditIncidentOpened, model.AuditDiagnosisCompleted, model.AuditRemediationDryRun, model.AuditPROpened, model.AuditEscalated} {
		if !kinds[need] {
			t.Fatalf("missing %s in %+v", need, kinds)
		}
	}

	again := exampleSignal()
	res2, err := p.HandleSignal(context.Background(), again)
	if err != nil {
		t.Fatal(err)
	}
	if res2.Incident.Count != 2 || res2.Incident.ID != inc.ID {
		t.Fatalf("%+v", res2.Incident)
	}
	if len(mem.List()) != 1 {
		t.Fatal("duplicate signal opened another pull request")
	}
}

func TestPollerFeedsPipeline(t *testing.T) {
	p, rt, _, _ := testPipe(t, false)
	rt.Signals = []model.Signal{*exampleSignal()}
	n, err := (&Poller{KQL: "exceptions | take 10", RT: rt, Pipe: p}).Once(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got := p.Store.List(); len(got) != 1 || got[0].Status != model.StatusPROpened {
		t.Fatalf("%+v", got)
	}
}

func TestCapacityStaysAtTier2(t *testing.T) {
	p, rt, mem, _ := testPipe(t, false)
	_, err := p.HandleSignal(context.Background(), &model.Signal{
		Source: model.SourceMonitorAlert, Severity: model.Sev2,
		ResourceID: config.DemoCapacityID, ResourceName: "example-worker", ResourceType: model.TypeWebSite,
		Message: "CPU saturation on the worker plan",
	})
	if err != nil {
		t.Fatal(err)
	}
	list := p.Store.List()
	if len(list) != 1 {
		t.Fatal(len(list))
	}
	inc := list[0]
	if inc.PullRequest != nil || len(mem.List()) != 0 {
		t.Fatal("capacity incident opened a code change")
	}
	if len(inc.Actions) != 1 || inc.Actions[0].Kind != model.ActionScaleOut || inc.Actions[0].Outcome != "dry_run" {
		t.Fatalf("%+v", inc.Actions)
	}
	if len(rt.Snapshot()) != 0 {
		t.Fatal(rt.Snapshot())
	}
}

func TestKillSwitchStopsAutomation(t *testing.T) {
	p, rt, mem, _ := testPipe(t, true)
	res, err := p.HandleSignal(context.Background(), exampleSignal())
	if err != nil {
		t.Fatal(err)
	}
	if res.Incident.Hypothesis == nil {
		t.Fatal("tier 1 should still diagnose")
	}
	if len(res.Incident.Actions) != 0 || res.Incident.PullRequest != nil || len(mem.List()) != 0 {
		t.Fatalf("%+v", res.Incident)
	}
	if len(rt.Snapshot()) != 0 {
		t.Fatal(rt.Snapshot())
	}
	if res.Incident.Status != model.StatusAwaitingHuman {
		t.Fatal(res.Incident.Status)
	}
}

func testPipe(t *testing.T, killed bool) (*Pipeline, *fake.Runtime, *fix.Memory, *audit.Log) {
	t.Helper()
	cfg := config.Demo()
	now := time.Date(2026, 11, 13, 14, 2, 0, 0, time.UTC)
	rt := fake.New(func() time.Time { return now }).WithExampleDeploy(config.DemoResourceID)
	log, err := audit.New(t.TempDir()+"/audit.jsonl", 500, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	mem := fix.NewMemory()
	gate := safety.New(killed)
	st := store.New(func() time.Time { return now })
	p := &Pipeline{
		Store:    st,
		Audit:    log,
		Gate:     gate,
		Policy:   policy.New(cfg),
		Gather:   diagnose.Gatherer{RT: rt, Window: cfg.Escalation.DeployWindow, Now: func() time.Time { return now }},
		Diagnose: diagnose.Heuristic{},
		Exec:     &remediate.Executor{RT: rt, Gate: gate, Audit: log, Now: func() time.Time { return now }},
		Fix:      fix.Service{Provider: mem},
		Notify:   []notify.Delivery{&notify.Recorder{}},
		DryRun:   cfg.EffectiveDryRun,
		Now:      func() time.Time { return now },
	}
	return p, rt, mem, log
}

type failModel struct{}

func (failModel) Complete(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("model down")
}

func TestModelPatchFallbackIsAudited(t *testing.T) {
	p, _, mem, log := testPipe(t, false)
	p.Fix.Model = failModel{}
	p.Fix.Fix.Mode = config.FixModeModelPatch
	res, err := p.HandleSignal(context.Background(), exampleSignal())
	if err != nil {
		t.Fatal(err)
	}
	if res.Incident.Status != model.StatusPROpened || res.Incident.PullRequest == nil {
		t.Fatalf("%+v", res.Incident)
	}
	if res.Incident.PullRequest.Mode != config.FixModeTemplate {
		t.Fatal(res.Incident.PullRequest.Mode)
	}
	kinds := map[string]string{}
	for _, e := range log.ByIncident(res.Incident.ID) {
		kinds[e.Kind] = e.Message
	}
	if !strings.Contains(kinds[model.AuditFixFallback], "model down") {
		t.Fatalf("%v", kinds)
	}
	pr, ok := mem.Get(res.Incident.PullRequest.ID)
	if !ok || !strings.Contains(pr.Body, "Mode fallback") {
		t.Fatal(pr.Body)
	}
}

func TestIssueModeOpensNoPullRequest(t *testing.T) {
	p, _, mem, log := testPipe(t, false)
	cfg := config.Demo()
	cfg.Resources[0].FixMode = "copilot_issue"
	p.Policy = policy.New(cfg)
	p.Fix.Issues = mem
	p.Fix.Source = mem
	p.Fix.Fix.Mode = config.FixModeTemplate
	res, err := p.HandleSignal(context.Background(), exampleSignal())
	if err != nil {
		t.Fatal(err)
	}
	inc := res.Incident
	if inc.Status != model.StatusIssueOpened || inc.PullRequest != nil || inc.Issue == nil {
		t.Fatalf("status=%s pr=%v issue=%v", inc.Status, inc.PullRequest, inc.Issue)
	}
	if len(mem.List()) != 0 {
		t.Fatal("issue mode created a pull request")
	}
	if inc.Issue.Agent != "copilot" {
		t.Fatal(inc.Issue.Agent)
	}
	kinds := map[string]bool{}
	for _, e := range log.ByIncident(inc.ID) {
		kinds[e.Kind] = true
	}
	if !kinds[model.AuditIssueOpened] {
		t.Fatalf("%v", kinds)
	}
}

func exampleSignal() *model.Signal {
	return &model.Signal{
		Source:        model.SourceMonitorAlert,
		Severity:      model.Sev2,
		ResourceID:    config.DemoResourceID,
		ResourceName:  "example-api",
		ResourceType:  model.TypeWebSite,
		Operation:     "POST /v1/orders",
		ExceptionType: "System.NullReferenceException",
		TopFrame:      "OrderService.Place",
		Message:       "Object reference not set to an instance of an object.",
		StackTrace:    "at Example.Orders.OrderService.Place(OrderService.cs:88)",
		ErrorRate:     0.184,
	}
}
