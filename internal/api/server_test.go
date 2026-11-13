package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/larryjoelane/healbot/internal/audit"
	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/demo"
	"github.com/larryjoelane/healbot/internal/diagnose"
	"github.com/larryjoelane/healbot/internal/fix"
	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/notify"
	"github.com/larryjoelane/healbot/internal/pipeline"
	"github.com/larryjoelane/healbot/internal/platform/fake"
	"github.com/larryjoelane/healbot/internal/policy"
	"github.com/larryjoelane/healbot/internal/remediate"
	"github.com/larryjoelane/healbot/internal/safety"
	"github.com/larryjoelane/healbot/internal/store"
)

func TestWebhookToPullRequest(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/monitor?sync=1", bytes.NewReader(demo.MonitorBody()))
	req.Header.Set("X-Healbot-Token", "demo-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var result pipeline.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Incident.Status != model.StatusPROpened {
		t.Fatalf("%+v", result.Incident)
	}
	if len(result.Incident.Actions) != 1 || result.Incident.Actions[0].Outcome != "dry_run" {
		t.Fatalf("%+v", result.Incident.Actions)
	}
	page := httptest.NewRequest(http.MethodGet, "/incidents/"+result.Incident.ID, nil)
	pageRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(pageRec, page)
	if pageRec.Code != 200 || !bytes.Contains(pageRec.Body.Bytes(), []byte("OrderService.Place")) {
		t.Fatalf("%d %s", pageRec.Code, pageRec.Body.String())
	}
	if !bytes.Contains(pageRec.Body.Bytes(), []byte("not merged")) && !bytes.Contains(pageRec.Body.Bytes(), []byte("does not merge")) {
		t.Fatal("page should say the pull request is not merged")
	}
}

func TestWebhookAuth(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/monitor", bytes.NewReader(demo.MonitorBody()))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatal(rec.Code)
	}
}

func TestKillSwitchAPI(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/control/kill", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	alert := httptest.NewRequest(http.MethodPost, "/v1/ingest/monitor?sync=1", bytes.NewReader(demo.MonitorBody()))
	alert.Header.Set("X-Healbot-Token", "demo-token")
	got := httptest.NewRecorder()
	srv.Handler().ServeHTTP(got, alert)
	var result pipeline.Result
	if err := json.Unmarshal(got.Body.Bytes(), &result); err != nil {
		t.Fatal(err, got.Body.String())
	}
	if result.Incident.PullRequest != nil || result.Incident.Status != model.StatusAwaitingHuman {
		t.Fatalf("%+v", result.Incident)
	}
}

func testServer(t *testing.T) *Server {
	t.Helper()
	cfg := config.Demo()
	now := time.Date(2026, 11, 13, 14, 2, 0, 0, time.UTC)
	rt := fake.New(func() time.Time { return now }).WithExampleDeploy(config.DemoResourceID)
	log, err := audit.New(t.TempDir()+"/audit.jsonl", 200, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	st := store.New(func() time.Time { return now })
	gate := safety.New(false)
	mem := fix.NewMemory()
	pipe := &pipeline.Pipeline{
		Store: st, Audit: log, Gate: gate, Policy: policy.New(cfg),
		Gather:   diagnose.Gatherer{RT: rt, Window: cfg.Escalation.DeployWindow, Now: func() time.Time { return now }},
		Diagnose: diagnose.Heuristic{},
		Exec:     &remediate.Executor{RT: rt, Gate: gate, Audit: log, Now: func() time.Time { return now }},
		Fix:      fix.Service{Provider: mem},
		Notify:   []notify.Delivery{&notify.Recorder{}},
		DryRun:   cfg.EffectiveDryRun,
		Now:      func() time.Time { return now },
	}
	return New(Deps{Config: cfg, Store: st, Audit: log, Gate: gate, Pipe: pipe, Pulls: mem, Fake: rt})
}

func TestAzureModeFailsClosedWithoutToken(t *testing.T) {
	srv := testServer(t)
	srv.Config.Mode = "azure"
	srv.Config.Server.WebhookToken = ""
	srv.Config.Server.AdminToken = ""

	ingest := httptest.NewRequest(http.MethodPost, "/v1/ingest/monitor", bytes.NewReader(demo.MonitorBody()))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, ingest)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ingest %d %s", rec.Code, rec.Body.String())
	}

	kill := httptest.NewRequest(http.MethodPost, "/v1/control/kill", nil)
	krec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(krec, kill)
	if krec.Code != http.StatusUnauthorized {
		t.Fatalf("kill %d %s", krec.Code, krec.Body.String())
	}
}

func TestDemoPage(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("HEALBOT")) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	_ = context.Background()
}

func TestDemoConsoleEmbedsAdminToken(t *testing.T) {
	srv := testServer(t)
	srv.Config.Server.AdminToken = "demo-token"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte("demo-token")) {
		t.Fatalf("demo console should embed the admin token, %d %s", rec.Code, rec.Body.String())
	}
}

func TestAzureConsoleOmitsAdminToken(t *testing.T) {
	srv := testServer(t)
	srv.Config.Mode = "azure"
	srv.Config.Server.AdminToken = "not-a-real-token-value"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("not-a-real-token-value")) {
		t.Fatal("azure console embedded the admin token")
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`type="password"`)) {
		t.Fatal("azure console should ask for the admin token")
	}
}
