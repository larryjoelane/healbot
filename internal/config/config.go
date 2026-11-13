// Package config loads Healbot's runtime configuration.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
	"gopkg.in/yaml.v3"
)

// Config is the process configuration. Dry-run defaults on.
type Config struct {
	Server Server `yaml:"server"`
	Mode   string `yaml:"mode"`
	// DryRun defaults to true when omitted. A global true value cannot be
	// overridden by a resource: it is the ceiling for live mutations.
	DryRun     *bool      `yaml:"dry_run"`
	Azure      Azure      `yaml:"azure"`
	Audit      Audit      `yaml:"audit"`
	Ingest     Ingest     `yaml:"ingest"`
	Safety     Safety     `yaml:"safety"`
	Escalation Escalation `yaml:"escalation"`
	Resources  []Resource `yaml:"resources"`
	Notify     Notify     `yaml:"notify"`
	LLM        LLM        `yaml:"llm"`
	Repo       Repo       `yaml:"repo"`
	Poller     Poller     `yaml:"poller"`
}

// Server is the HTTP listener.
type Server struct {
	Addr         string `yaml:"addr"`
	WebhookToken string `yaml:"webhook_token"`
	AdminToken   string `yaml:"admin_token"`
}

// Azure selects the subscription and workspace used with DefaultAzureCredential.
type Azure struct {
	SubscriptionID string `yaml:"subscription_id"`
	WorkspaceID    string `yaml:"workspace_id"`
}

// Audit configures the decision log.
type Audit struct {
	Path      string `yaml:"path"`
	Sync      bool   `yaml:"sync"`
	MaxMemory int    `yaml:"max_memory"`
}

// Ingest sizes the worker pools.
type Ingest struct {
	Workers         int `yaml:"workers"`
	Queue           int `yaml:"queue"`
	DiagnoseWorkers int `yaml:"diagnose_workers"`
	DiagnoseQueue   int `yaml:"diagnose_queue"`
}

// Safety holds the global breaker and rate defaults.
type Safety struct {
	Cooldown        time.Duration `yaml:"-"`
	CooldownRaw     string        `yaml:"cooldown"`
	MaxPerHour      int           `yaml:"max_per_hour"`
	BreakerFailures int           `yaml:"breaker_failures"`
	BreakerOpenFor  time.Duration `yaml:"-"`
	BreakerOpenRaw  string        `yaml:"breaker_open_for"`
}

// Escalation is the rule list.
type Escalation struct {
	DeployWindow    time.Duration `yaml:"-"`
	DeployWindowRaw string        `yaml:"deploy_window"`
	NotifyCounts    []int         `yaml:"notify_counts"`
	Rules           []Rule        `yaml:"rules"`
}

// Rule is one escalation predicate.
type Rule struct {
	Name                 string   `yaml:"name"`
	Tier                 int      `yaml:"tier"`
	MinSeverity          string   `yaml:"min_severity"`
	MinCount             int      `yaml:"min_count"`
	MinErrorRate         float64  `yaml:"min_error_rate"`
	RequiresRecentDeploy bool     `yaml:"requires_recent_deploy"`
	RequiresLikelyCode   bool     `yaml:"requires_likely_code"`
	Categories           []string `yaml:"categories"`
	WhenTier2            []string `yaml:"when_tier2"`
}

// Resource is the per-resource tier policy.
type Resource struct {
	ID                 string   `yaml:"id"`
	MaxTier            int      `yaml:"max_tier"`
	DryRun             *bool    `yaml:"dry_run"`
	AllowedActions     []string `yaml:"allowed_actions"`
	Cooldown           string   `yaml:"cooldown"`
	MaxActionsPerHour  int      `yaml:"max_actions_per_hour"`
	MaxInstances       int      `yaml:"max_instances"`
	KnownGoodSlot      string   `yaml:"known_good_slot"`
	KnownGoodRevision  string   `yaml:"known_good_revision"`
	ConsumerResourceID string   `yaml:"consumer_resource_id"`
	HealthURL          string   `yaml:"health_url"`
}

// Notify configures outbound channels. All of them implement one interface.
type Notify struct {
	Stdout      bool         `yaml:"stdout"`
	Webhooks    []Webhook    `yaml:"webhooks"`
	GitHubIssue *GitHubIssue `yaml:"github_issue"`
	Email       *Email       `yaml:"email"`
}

// Webhook is a Teams, Slack, or generic JSON endpoint.
type Webhook struct {
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	Format string `yaml:"format"`
}

// GitHubIssue opens a tracking issue. It does not merge code.
type GitHubIssue struct {
	Owner    string `yaml:"owner"`
	Repo     string `yaml:"repo"`
	TokenEnv string `yaml:"token_env"`
	BaseURL  string `yaml:"base_url"`
}

// Email is an optional SMTP delivery. The pipeline depends on the interface.
type Email struct {
	Addr        string   `yaml:"addr"`
	From        string   `yaml:"from"`
	To          []string `yaml:"to"`
	Username    string   `yaml:"username"`
	PasswordEnv string   `yaml:"password_env"`
}

// LLM selects the diagnosis provider.
type LLM struct {
	Provider   string `yaml:"provider"`
	Endpoint   string `yaml:"endpoint"`
	Deployment string `yaml:"deployment"`
	APIVersion string `yaml:"api_version"`
	APIKeyEnv  string `yaml:"api_key_env"`
}

// Repo selects where tier-3 pull requests are opened.
type Repo struct {
	Provider string     `yaml:"provider"`
	GitHub   GitHubRepo `yaml:"github"`
	AzDO     AzDORepo   `yaml:"azure_devops"`
}

// GitHubRepo is a GitHub repository.
type GitHubRepo struct {
	Owner    string `yaml:"owner"`
	Repo     string `yaml:"repo"`
	TokenEnv string `yaml:"token_env"`
	BaseURL  string `yaml:"base_url"`
}

// AzDORepo is an Azure DevOps repository.
type AzDORepo struct {
	Org      string `yaml:"org"`
	Project  string `yaml:"project"`
	Repo     string `yaml:"repo"`
	TokenEnv string `yaml:"token_env"`
	BaseURL  string `yaml:"base_url"`
}

// Poller optionally runs a Log Analytics query on an interval.
type Poller struct {
	Enabled  bool   `yaml:"enabled"`
	Every    string `yaml:"every"`
	Lookback string `yaml:"lookback"`
	KQL      string `yaml:"kql"`
}

// demoWebhookToken is a local placeholder, not a credential.
// Demo mode may use it. Azure mode rejects it.
const demoWebhookToken = "demo-token"

// DemoResourceID is the app used by the local simulator.
const DemoResourceID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/example-rg/providers/Microsoft.Web/sites/example-api"

// DemoCapacityID is a second demo resource that should only scale, not open a PR.
const DemoCapacityID = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/example-rg/providers/Microsoft.Web/sites/example-worker"

// Load reads YAML and applies environment overrides.
// An empty path uses Demo. If environment overrides then leave demo mode
// without setting HEALBOT_WEBHOOK_TOKEN, the demo placeholder is cleared
// so azure mode starts with an empty token and fails closed.
func Load(path string) (Config, error) {
	cfg := Demo()
	fromDemoDefault := path == ""
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("config: %w", err)
		}
		cfg = Config{}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("config: %w", err)
		}
		fromDemoDefault = false
	}
	cfg.applyDefaults()
	cfg.applyEnv()
	if fromDemoDefault && cfg.Mode != "demo" && os.Getenv("HEALBOT_WEBHOOK_TOKEN") == "" {
		cfg.Server.WebhookToken = ""
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Demo is the subscription-free configuration used by the simulator.
func Demo() Config {
	dry := true
	cfg := Config{
		// demo-token is demo-only. See demoWebhookToken.
		Server: Server{Addr: ":43117", WebhookToken: demoWebhookToken},
		Mode:   "demo",
		DryRun: boolPtr(true),
		Audit:  Audit{Path: "data/audit.jsonl", Sync: true, MaxMemory: 2000},
		Ingest: Ingest{Workers: 4, Queue: 256, DiagnoseWorkers: 2, DiagnoseQueue: 64},
		Safety: Safety{CooldownRaw: "10m", MaxPerHour: 3, BreakerFailures: 2, BreakerOpenRaw: "30m"},
		Escalation: Escalation{
			DeployWindowRaw: "2h",
			NotifyCounts:    []int{1, 10, 50, 100},
			Rules:           DefaultRules(),
		},
		Resources: []Resource{
			{
				ID:                DemoResourceID,
				MaxTier:           3,
				DryRun:            &dry,
				AllowedActions:    []string{"restart", "scale_out", "slot_swap", "restart_consumer"},
				Cooldown:          "10m",
				MaxActionsPerHour: 3,
				MaxInstances:      5,
				KnownGoodSlot:     "staging",
			},
			{
				ID:                DemoCapacityID,
				MaxTier:           2,
				DryRun:            &dry,
				AllowedActions:    []string{"scale_out", "restart"},
				Cooldown:          "10m",
				MaxActionsPerHour: 3,
				MaxInstances:      4,
			},
		},
		Notify: Notify{Stdout: true},
		LLM:    LLM{Provider: "heuristic", APIVersion: "2024-10-21", APIKeyEnv: "AZURE_OPENAI_API_KEY"},
		Repo: Repo{
			Provider: "mock",
			GitHub:   GitHubRepo{TokenEnv: "GITHUB_TOKEN"},
			AzDO:     AzDORepo{TokenEnv: "AZDO_TOKEN"},
		},
		Poller: Poller{
			Enabled:  false,
			Every:    "1m",
			Lookback: "15m",
			KQL: `exceptions
| where timestamp > ago(15m)
| extend ResourceId = tostring(customDimensions["ResourceId"])
| project timestamp, problemId, outerMessage, operation_Name, ResourceId, details
| take 200`,
		},
	}
	cfg.applyDefaults()
	return cfg
}

// DefaultRules is the escalation policy shipped with the demo and the example config.
func DefaultRules() []Rule {
	return []Rule{
		{Name: "diagnose", Tier: 1, MinSeverity: "sev3", MinCount: 1},
		{Name: "rollback-after-deploy", Tier: 2, MinSeverity: "sev2", MinCount: 1, RequiresRecentDeploy: true, Categories: []string{"code", "deployment", "config"}},
		{Name: "capacity-or-dependency", Tier: 2, MinSeverity: "sev2", MinCount: 1, Categories: []string{"capacity", "dependency"}},
		{Name: "repeat-offender", Tier: 2, MinSeverity: "sev3", MinCount: 5},
		{Name: "code-pull-request", Tier: 3, MinSeverity: "sev2", MinCount: 1, RequiresLikelyCode: true, WhenTier2: []string{"dry_run", "failed", "not_hold", "unverified", "skipped", "rate_limited", "cooldown", "breaker_open", "at_capacity"}},
	}
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":43117"
	}
	if c.Mode == "" {
		c.Mode = "demo"
	}
	if c.Ingest.Workers < 1 {
		c.Ingest.Workers = 4
	}
	if c.Ingest.Queue < 1 {
		c.Ingest.Queue = 256
	}
	if c.Ingest.DiagnoseWorkers < 1 {
		c.Ingest.DiagnoseWorkers = 2
	}
	if c.Ingest.DiagnoseQueue < 1 {
		c.Ingest.DiagnoseQueue = 64
	}
	if c.Audit.MaxMemory < 1 {
		c.Audit.MaxMemory = 2000
	}
	if c.Safety.MaxPerHour < 1 {
		c.Safety.MaxPerHour = 3
	}
	if c.Safety.BreakerFailures < 1 {
		c.Safety.BreakerFailures = 2
	}
	if c.Safety.CooldownRaw == "" {
		c.Safety.CooldownRaw = "10m"
	}
	if c.Safety.BreakerOpenRaw == "" {
		c.Safety.BreakerOpenRaw = "30m"
	}
	if c.Escalation.DeployWindowRaw == "" {
		c.Escalation.DeployWindowRaw = "2h"
	}
	if len(c.Escalation.NotifyCounts) == 0 {
		c.Escalation.NotifyCounts = []int{1, 10, 50, 100}
	}
	if len(c.Escalation.Rules) == 0 {
		c.Escalation.Rules = DefaultRules()
	}
	if c.LLM.Provider == "" {
		c.LLM.Provider = "heuristic"
	}
	if c.LLM.APIVersion == "" {
		c.LLM.APIVersion = "2024-10-21"
	}
	if c.Repo.Provider == "" {
		c.Repo.Provider = "mock"
	}
	c.Safety.Cooldown = mustDuration(c.Safety.CooldownRaw, 10*time.Minute)
	c.Safety.BreakerOpenFor = mustDuration(c.Safety.BreakerOpenRaw, 30*time.Minute)
	c.Escalation.DeployWindow = mustDuration(c.Escalation.DeployWindowRaw, 2*time.Hour)
	for i := range c.Resources {
		if c.Resources[i].MaxTier == 0 {
			c.Resources[i].MaxTier = 1
		}
		if c.Resources[i].MaxInstances == 0 {
			c.Resources[i].MaxInstances = 5
		}
		if c.Resources[i].KnownGoodSlot == "" && strings.Contains(c.Resources[i].ID, "Microsoft.Web/sites") {
			c.Resources[i].KnownGoodSlot = "staging"
		}
	}
}

func (c *Config) applyEnv() {
	if v := os.Getenv("HEALBOT_ADDR"); v != "" {
		c.Server.Addr = v
	}
	if v := os.Getenv("HEALBOT_WEBHOOK_TOKEN"); v != "" {
		c.Server.WebhookToken = v
	}
	if v := os.Getenv("HEALBOT_ADMIN_TOKEN"); v != "" {
		c.Server.AdminToken = v
	}
	if v := os.Getenv("HEALBOT_MODE"); v != "" {
		c.Mode = v
	}
	if v := os.Getenv("HEALBOT_DRY_RUN"); v != "" {
		c.DryRun = boolPtr(strings.EqualFold(v, "true") || v == "1")
	}
	if v := os.Getenv("HEALBOT_SUBSCRIPTION_ID"); v != "" {
		c.Azure.SubscriptionID = v
	}
	if v := os.Getenv("AZURE_SUBSCRIPTION_ID"); v != "" && c.Azure.SubscriptionID == "" {
		c.Azure.SubscriptionID = v
	}
	if v := os.Getenv("HEALBOT_WORKSPACE_ID"); v != "" {
		c.Azure.WorkspaceID = v
	}
	if v := os.Getenv("HEALBOT_AUDIT_PATH"); v != "" {
		c.Audit.Path = v
	}
	if v := os.Getenv("AZURE_OPENAI_ENDPOINT"); v != "" {
		c.LLM.Endpoint = v
	}
	if v := os.Getenv("AZURE_OPENAI_DEPLOYMENT"); v != "" {
		c.LLM.Deployment = v
	}
	if v := os.Getenv("HEALBOT_LLM_PROVIDER"); v != "" {
		c.LLM.Provider = v
	}
}

func (c Config) validate() error {
	switch c.Mode {
	case "demo", "azure":
	default:
		return fmt.Errorf("config: mode must be demo or azure, got %q", c.Mode)
	}
	if c.Mode == "azure" && c.Azure.SubscriptionID == "" {
		return fmt.Errorf("config: azure mode requires azure.subscription_id or HEALBOT_SUBSCRIPTION_ID")
	}
	if c.Mode == "azure" && (c.Server.WebhookToken == demoWebhookToken || c.Server.AdminToken == demoWebhookToken) {
		return fmt.Errorf("config: %q is a demo-only placeholder and is rejected in azure mode; set HEALBOT_WEBHOOK_TOKEN and HEALBOT_ADMIN_TOKEN", demoWebhookToken)
	}
	for _, r := range c.Resources {
		if r.MaxTier < 1 || r.MaxTier > 3 {
			return fmt.Errorf("config: resource %s max_tier must be 1, 2, or 3", r.ID)
		}
		for _, a := range r.AllowedActions {
			if !model.ActionKind(a).Valid() {
				return fmt.Errorf("config: resource %s has unknown action %q", r.ID, a)
			}
		}
	}
	for _, rule := range c.Escalation.Rules {
		if rule.Tier < 1 || rule.Tier > 3 {
			return fmt.Errorf("config: rule %s tier must be 1, 2, or 3", rule.Name)
		}
		if rule.MinSeverity != "" {
			if _, ok := model.ParseSeverity(rule.MinSeverity); !ok {
				return fmt.Errorf("config: rule %s has bad min_severity %q", rule.Name, rule.MinSeverity)
			}
		}
	}
	return nil
}

// GlobalDryRun reports the process-wide dry-run ceiling. Omitted config means on.
func (c Config) GlobalDryRun() bool {
	if c.DryRun == nil {
		return true
	}
	return *c.DryRun
}

// EffectiveDryRun reports whether a resource may perform a live mutation.
// The global dry-run flag is a ceiling: it cannot be overridden per resource.
func (c Config) EffectiveDryRun(res Resource) bool {
	if c.GlobalDryRun() {
		return true
	}
	if res.DryRun == nil {
		return true
	}
	return *res.DryRun
}

func boolPtr(v bool) *bool { return &v }

func mustDuration(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return d
}

// Duration parses a config duration with a fallback.
func Duration(raw string, fallback time.Duration) time.Duration {
	return mustDuration(raw, fallback)
}

// EnvInt reads an integer environment variable.
func EnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
