// Package app wires configuration, Azure or the fake runtime, and the HTTP server.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/larryjoelane/healbot/internal/api"
	"github.com/larryjoelane/healbot/internal/audit"
	"github.com/larryjoelane/healbot/internal/azure"
	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/diagnose"
	"github.com/larryjoelane/healbot/internal/fix"
	"github.com/larryjoelane/healbot/internal/ingest"
	"github.com/larryjoelane/healbot/internal/notify"
	"github.com/larryjoelane/healbot/internal/pipeline"
	"github.com/larryjoelane/healbot/internal/platform"
	"github.com/larryjoelane/healbot/internal/platform/fake"
	"github.com/larryjoelane/healbot/internal/policy"
	"github.com/larryjoelane/healbot/internal/remediate"
	"github.com/larryjoelane/healbot/internal/safety"
	"github.com/larryjoelane/healbot/internal/store"
)

// App is a running Healbot process.
type App struct {
	Config  config.Config
	Store   *store.Store
	Audit   *audit.Log
	Gate    *safety.Gate
	Pipe    *pipeline.Pipeline
	Pulls   *fix.Memory
	Runtime platform.Runtime
	Fake    *fake.Runtime
	Pool    *ingest.Pool
	Poller  *pipeline.Poller
	Handler http.Handler
	log     *audit.Log
}

// Build assembles the process. Demo mode never contacts Azure.
func Build(cfg config.Config) (*App, error) {
	log, err := audit.New(cfg.Audit.Path, cfg.Audit.MaxMemory, cfg.Audit.Sync)
	if err != nil {
		return nil, err
	}
	st := store.New(time.Now)
	gate := safety.New(false)
	eng := policy.New(cfg)
	var rt platform.Runtime
	var fk *fake.Runtime
	var cred azcore.TokenCredential
	if cfg.Mode == "azure" {
		c, err := azure.NewCredential()
		if err != nil {
			_ = log.Close()
			return nil, fmt.Errorf("default azure credential: %w", err)
		}
		cred = c
		real, err := azure.NewRuntime(cfg.Azure.SubscriptionID, cred, cfg.Azure.WorkspaceID, nil)
		if err != nil {
			_ = log.Close()
			return nil, err
		}
		rt = real
	} else {
		fk = fake.New(time.Now).WithExampleDeploy(config.DemoResourceID)
		rt = fk
	}
	var provider fix.Provider
	mem := fix.NewMemory()
	switch cfg.Repo.Provider {
	case "github":
		provider = &fix.GitHub{
			BaseURL: cfg.Repo.GitHub.BaseURL,
			Owner:   cfg.Repo.GitHub.Owner,
			Repo:    cfg.Repo.GitHub.Repo,
			Token:   os.Getenv(envOr(cfg.Repo.GitHub.TokenEnv, "GITHUB_TOKEN")),
		}
	case "azure_devops", "azdo":
		provider = &fix.AzureDevOps{
			BaseURL: cfg.Repo.AzDO.BaseURL,
			Org:     cfg.Repo.AzDO.Org,
			Project: cfg.Repo.AzDO.Project,
			Repo:    cfg.Repo.AzDO.Repo,
			Token:   os.Getenv(envOr(cfg.Repo.AzDO.TokenEnv, "AZDO_TOKEN")),
		}
	default:
		provider = mem
	}
	var diag diagnose.Diagnoser = diagnose.Heuristic{}
	if cfg.LLM.Provider == "azure_openai" && cfg.LLM.Endpoint != "" && cfg.LLM.Deployment != "" {
		llm := &diagnose.AzureOpenAI{
			Endpoint:   cfg.LLM.Endpoint,
			Deployment: cfg.LLM.Deployment,
			APIVersion: cfg.LLM.APIVersion,
			Key:        os.Getenv(envOr(cfg.LLM.APIKeyEnv, "AZURE_OPENAI_API_KEY")),
		}
		if llm.Key == "" {
			llm.Cred = cred
		}
		diag = diagnose.Service{LLM: llm}
	}
	exec := &remediate.Executor{
		RT: rt, Gate: gate, Audit: log, Now: time.Now,
		Limits: func(res config.Resource) safety.Limits {
			cd := cfg.Safety.Cooldown
			if res.Cooldown != "" {
				cd = config.Duration(res.Cooldown, cd)
			}
			max := cfg.Safety.MaxPerHour
			if res.MaxActionsPerHour > 0 {
				max = res.MaxActionsPerHour
			}
			return safety.Limits{
				Cooldown: cd, MaxPerHour: max,
				BreakerFailures: cfg.Safety.BreakerFailures, BreakerOpenFor: cfg.Safety.BreakerOpenFor,
			}
		},
	}
	pipe := &pipeline.Pipeline{
		Store: st, Audit: log, Gate: gate, Policy: eng,
		Gather:   diagnose.Gatherer{RT: rt, Window: cfg.Escalation.DeployWindow, Now: time.Now},
		Diagnose: diag,
		Exec:     exec,
		Fix:      fix.Service{Provider: provider},
		Notify:   channels(cfg),
		DryRun:   cfg.EffectiveDryRun,
		Now:      time.Now,
	}
	pool := ingest.NewPool(cfg.Ingest.DiagnoseWorkers, cfg.Ingest.DiagnoseQueue)
	var poller *pipeline.Poller
	if cfg.Poller.Enabled {
		poller = &pipeline.Poller{
			Every: config.Duration(cfg.Poller.Every, time.Minute),
			KQL:   cfg.Poller.KQL,
			RT:    rt,
			Pipe:  pipe,
		}
	}
	a := &App{
		Config: cfg, Store: st, Audit: log, Gate: gate, Pipe: pipe,
		Pulls: mem, Runtime: rt, Fake: fk, Pool: pool, Poller: poller, log: log,
	}
	a.Handler = api.New(api.Deps{
		Config: cfg, Store: st, Audit: log, Gate: gate, Pipe: pipe,
		Pulls: mem, Fake: fk, Pool: pool,
	}).Handler()
	return a, nil
}

func channels(cfg config.Config) []notify.Delivery {
	var out []notify.Delivery
	if cfg.Notify.Stdout {
		out = append(out, notify.Log{Printf: func(format string, args ...any) {
			slog.Info(fmt.Sprintf(format, args...))
		}})
	}
	for _, h := range cfg.Notify.Webhooks {
		if h.URL == "" {
			continue
		}
		out = append(out, notify.Webhook{HookName: h.Name, URL: h.URL, Format: h.Format})
	}
	if cfg.Notify.GitHubIssue != nil && cfg.Notify.GitHubIssue.Owner != "" {
		g := cfg.Notify.GitHubIssue
		out = append(out, notify.GitHubIssue{
			BaseURL: g.BaseURL, Owner: g.Owner, Repo: g.Repo,
			Token: os.Getenv(envOr(g.TokenEnv, "GITHUB_TOKEN")),
		})
	}
	if cfg.Notify.Email != nil && cfg.Notify.Email.Addr != "" {
		mail := cfg.Notify.Email
		sender := notify.SMTPMailer{Addr: mail.Addr, From: mail.From, To: mail.To}
		if mail.Username != "" {
			pass := os.Getenv(envOr(mail.PasswordEnv, "HEALBOT_SMTP_PASSWORD"))
			host := mail.Addr
			if i := strings.Index(host, ":"); i > 0 {
				host = host[:i]
			}
			sender.Auth = smtp.PlainAuth("", mail.Username, pass, host)
		}
		out = append(out, sender)
	}
	return out
}

func envOr(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

// Run serves HTTP until ctx is canceled.
func (a *App) Run(ctx context.Context) error {
	if a.Config.Server.WebhookToken == "" {
		if a.Config.Mode == "azure" {
			slog.Warn("webhook token is empty; azure mode rejects ingest until HEALBOT_WEBHOOK_TOKEN is set")
		} else {
			slog.Warn("webhook token is empty; ingest endpoints are unauthenticated")
		}
	}
	if a.Config.Mode == "azure" && a.Config.Server.AdminToken == "" {
		slog.Warn("admin token is empty; azure mode rejects control actions until HEALBOT_ADMIN_TOKEN is set")
	}
	slog.Info("healbot listening",
		"addr", a.Config.Server.Addr,
		"mode", a.Config.Mode,
		"dry_run", a.Config.GlobalDryRun(),
	)
	srv := &http.Server{
		Addr:              a.Config.Server.Addr,
		Handler:           a.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       time.Minute,
	}
	errc := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errc <- err
			return
		}
		errc <- nil
	}()
	if a.Poller != nil {
		go a.Poller.Run(ctx)
	}
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
		a.Close()
		return nil
	case err := <-errc:
		a.Close()
		return err
	}
}

// Close releases the pool and the audit log.
func (a *App) Close() {
	if a.Pool != nil {
		a.Pool.Close()
	}
	if a.log != nil {
		_ = a.log.Close()
	}
}
