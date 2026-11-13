// Package api is the HTTP surface: ingest, control, and the ops console.
package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/larryjoelane/healbot/internal/audit"
	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/demo"
	"github.com/larryjoelane/healbot/internal/fix"
	"github.com/larryjoelane/healbot/internal/ingest"
	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/pipeline"
	"github.com/larryjoelane/healbot/internal/platform/fake"
	"github.com/larryjoelane/healbot/internal/safety"
	"github.com/larryjoelane/healbot/internal/store"
	"github.com/larryjoelane/healbot/internal/ui"
)

// Deps are the process pieces the HTTP layer needs.
type Deps struct {
	Config config.Config
	Store  *store.Store
	Audit  *audit.Log
	Gate   *safety.Gate
	Pipe   *pipeline.Pipeline
	Pulls  *fix.Memory
	Fake   *fake.Runtime
	Pool   *ingest.Pool
}

// Server serves JSON and the console.
type Server struct{ Deps }

// New returns a server.
func New(d Deps) *Server { return &Server{Deps: d} }

// Handler is the full route table.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.health)
	mux.HandleFunc("POST /v1/ingest/monitor", s.ingestMonitor)
	mux.HandleFunc("POST /v1/ingest/logs", s.ingestLogs)
	mux.HandleFunc("GET /v1/incidents", s.listIncidents)
	mux.HandleFunc("GET /v1/incidents/{id}", s.getIncident)
	mux.HandleFunc("POST /v1/incidents/{id}/suppress", s.suppress)
	mux.HandleFunc("POST /v1/control/kill", s.kill)
	mux.HandleFunc("POST /v1/control/resume", s.resume)
	mux.HandleFunc("GET /v1/control/status", s.status)
	mux.HandleFunc("GET /v1/audit", s.audit)
	mux.HandleFunc("GET /v1/pulls", s.listPulls)
	mux.HandleFunc("GET /v1/pulls/{id}", s.getPull)
	mux.HandleFunc("GET /v1/debug/calls", s.debugCalls)
	mux.HandleFunc("POST /v1/demo/scenario", s.demo)
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /incidents/{id}", s.incidentPage)
	mux.HandleFunc("GET /audit", s.auditPage)
	mux.HandleFunc("GET /pulls/{id}", s.pullPage)
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ingestMonitor(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r, s.Config.Server.WebhookToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	sigs, err := ingest.ParseMonitorAlert(r.Body, time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.accept(w, r, sigs)
}

func (s *Server) ingestLogs(w http.ResponseWriter, r *http.Request) {
	if !s.allow(r, s.Config.Server.WebhookToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	sync := wantsSync(r)
	var accepted int
	err := ingest.ParseLogStream(r.Body, func(sig *model.Signal) error {
		cp := ingest.Detach(sig)
		accepted++
		if sync || s.Pool == nil {
			_, herr := s.Pipe.HandleSignal(r.Context(), &cp)
			return herr
		}
		return s.Pool.Submit(r.Context(), func() {
			_, _ = s.Pipe.HandleSignal(context.Background(), &cp)
		})
	})
	if err != nil {
		if errors.Is(err, ingest.ErrSaturated) {
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": accepted})
}

func (s *Server) accept(w http.ResponseWriter, r *http.Request, sigs []model.Signal) {
	sync := wantsSync(r)
	var last pipeline.Result
	for i := range sigs {
		sig := sigs[i]
		if sync || s.Pool == nil {
			res, err := s.Pipe.HandleSignal(r.Context(), &sig)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			last = res
			continue
		}
		if err := s.Pool.TrySubmit(func() {
			_, _ = s.Pipe.HandleSignal(context.Background(), &sig)
		}); err != nil {
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
	}
	if sync {
		writeJSON(w, http.StatusOK, last)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": len(sigs)})
}

func (s *Server) listIncidents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Store.List())
}

func (s *Server) getIncident(w http.ResponseWriter, r *http.Request) {
	inc, ok := s.Store.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, inc)
}

func (s *Server) suppress(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	id := r.PathValue("id")
	inc, ok := s.Store.Update(id, func(x *model.Incident) {
		x.Suppressed = true
		x.Status = model.StatusSuppressed
	})
	if !ok {
		http.NotFound(w, r)
		return
	}
	s.Audit.RecordSync(model.AuditEvent{IncidentID: id, Kind: model.AuditSuppressed, Actor: "human", Message: "operator suppressed automation"})
	if wantsHTML(r) {
		http.Redirect(w, r, "/incidents/"+id, http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, inc)
}

func (s *Server) kill(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	s.Gate.SetKilled(true)
	s.Audit.RecordSync(model.AuditEvent{Kind: model.AuditKillSwitch, Actor: "human", Message: "kill switch engaged"})
	s.respond(w, r, s.statusBody())
}

func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	s.Gate.SetKilled(false)
	s.Audit.RecordSync(model.AuditEvent{Kind: model.AuditKillSwitch, Actor: "human", Message: "kill switch released"})
	s.respond(w, r, s.statusBody())
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.statusBody())
}

func (s *Server) statusBody() map[string]any {
	return map[string]any{
		"mode":    s.Config.Mode,
		"dry_run": s.Config.GlobalDryRun(),
		"killed":  s.Gate.Killed(),
	}
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if id := r.URL.Query().Get("incident"); id != "" {
		writeJSON(w, http.StatusOK, s.Audit.ByIncident(id))
		return
	}
	writeJSON(w, http.StatusOK, s.Audit.List(limit))
}

func (s *Server) listPulls(w http.ResponseWriter, _ *http.Request) {
	if s.Pulls == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, s.Pulls.List())
}

func (s *Server) getPull(w http.ResponseWriter, r *http.Request) {
	if s.Pulls == nil {
		http.NotFound(w, r)
		return
	}
	p, ok := s.Pulls.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) debugCalls(w http.ResponseWriter, r *http.Request) {
	if s.Config.Mode != "demo" || s.Fake == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, s.Fake.Snapshot())
}

func (s *Server) demo(w http.ResponseWriter, r *http.Request) {
	if s.Config.Mode != "demo" {
		http.Error(w, "demo scenario is only available in demo mode", http.StatusForbidden)
		return
	}
	if !s.admin(w, r) {
		return
	}
	sigs, err := ingest.ParseMonitorAlert(bytes.NewReader(demo.MonitorBody()), time.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var last pipeline.Result
	for i := range sigs {
		sig := sigs[i]
		last, err = s.Pipe.HandleSignal(r.Context(), &sig)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if wantsHTML(r) {
		http.Redirect(w, r, "/incidents/"+last.Incident.ID, http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, last)
}

func (s *Server) home(w http.ResponseWriter, _ *http.Request) {
	ui.Render(w, s.page("Healbot", "home", model.Incident{}, fix.Pull{}))
}

func (s *Server) incidentPage(w http.ResponseWriter, r *http.Request) {
	inc, ok := s.Store.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	ui.Render(w, s.page(inc.Title, "incident", inc, fix.Pull{}))
}

func (s *Server) auditPage(w http.ResponseWriter, _ *http.Request) {
	ui.Render(w, s.page("Audit", "audit", model.Incident{}, fix.Pull{}))
}

func (s *Server) pullPage(w http.ResponseWriter, r *http.Request) {
	var pull fix.Pull
	if s.Pulls != nil {
		p, ok := s.Pulls.Get(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		pull = p
	}
	ui.Render(w, s.page(pull.Ref.Title, "pull", model.Incident{}, pull))
}

func (s *Server) page(title, view string, inc model.Incident, pull fix.Pull) ui.Page {
	var events []model.AuditEvent
	if view == "incident" {
		events = s.Audit.ByIncident(inc.ID)
	} else {
		events = s.Audit.List(40)
	}
	// The console is unauthenticated. Demo mode embeds the well-known
	// placeholder so the local buttons work. Any other mode leaves the
	// token out of the HTML; the operator types it into the control form.
	admin := ""
	if s.Config.Mode == "demo" {
		admin = s.Config.Server.AdminToken
	}
	return ui.Page{
		Title: title, View: view, Mode: s.Config.Mode,
		DryRun: s.Config.GlobalDryRun(), Killed: s.Gate.Killed(),
		Incidents: s.Store.List(), Incident: inc, Events: events, Pull: pull,
		AdminToken: admin,
	}
}

func (s *Server) admin(w http.ResponseWriter, r *http.Request) bool {
	_ = r.ParseForm()
	if s.allow(r, s.Config.Server.AdminToken) {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func (s *Server) allow(r *http.Request, token string) bool {
	if token == "" {
		// Demo mode stays open when a token is unset so the local console works.
		// Azure mode fails closed: an empty token does not authenticate.
		return s.Config.Mode != "azure"
	}
	got := r.Header.Get("X-Healbot-Token")
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	if got == "" {
		got = r.FormValue("token")
	}
	if len(got) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func (s *Server) respond(w http.ResponseWriter, r *http.Request, body any) {
	if wantsHTML(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func wantsSync(r *http.Request) bool {
	return r.URL.Query().Get("sync") == "1" || r.Header.Get("X-Healbot-Sync") == "1"
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}
