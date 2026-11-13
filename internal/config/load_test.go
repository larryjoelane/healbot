package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDemoFileLoads(t *testing.T) {
	t.Setenv("HEALBOT_MODE", "")
	t.Setenv("HEALBOT_DRY_RUN", "")
	t.Setenv("HEALBOT_SUBSCRIPTION_ID", "")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	cfg, err := Load(filepath.Join("..", "..", "config.demo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.GlobalDryRun() || cfg.Mode != "demo" {
		t.Fatalf("%+v", cfg)
	}
	if cfg.EffectiveDryRun(cfg.Resources[0]) != true {
		t.Fatal("demo resource must stay in dry run")
	}
	if cfg.Server.WebhookToken != demoWebhookToken {
		t.Fatalf("demo config token = %q", cfg.Server.WebhookToken)
	}
	if _, err := Load(filepath.Join("..", "..", "config.azure.example.yaml")); err == nil {
		t.Fatal("azure example has no subscription and must fail validation")
	}
}

func TestAzureModeDropsBuiltInDemoToken(t *testing.T) {
	t.Setenv("HEALBOT_MODE", "azure")
	t.Setenv("HEALBOT_WEBHOOK_TOKEN", "")
	t.Setenv("HEALBOT_ADMIN_TOKEN", "")
	t.Setenv("HEALBOT_DRY_RUN", "")
	t.Setenv("HEALBOT_SUBSCRIPTION_ID", "00000000-0000-0000-0000-000000000000")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "azure" || cfg.Server.WebhookToken != "" {
		t.Fatalf("mode=%s token=%q", cfg.Mode, cfg.Server.WebhookToken)
	}
}

func TestAzureModeRejectsDemoToken(t *testing.T) {
	t.Setenv("HEALBOT_MODE", "")
	t.Setenv("HEALBOT_WEBHOOK_TOKEN", "")
	t.Setenv("HEALBOT_ADMIN_TOKEN", "")
	t.Setenv("HEALBOT_SUBSCRIPTION_ID", "")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "mode: azure\nazure:\n  subscription_id: \"00000000-0000-0000-0000-000000000000\"\nserver:\n  webhook_token: demo-token\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("azure mode accepted the demo-only webhook token")
	}
}

func TestFixModeEnvOverrideAndRejection(t *testing.T) {
	t.Setenv("HEALBOT_MODE", "")
	t.Setenv("HEALBOT_DRY_RUN", "")
	t.Setenv("HEALBOT_SUBSCRIPTION_ID", "")
	t.Setenv("AZURE_SUBSCRIPTION_ID", "")
	t.Setenv("HEALBOT_WEBHOOK_TOKEN", "")
	t.Setenv("HEALBOT_ADMIN_TOKEN", "")
	t.Setenv("HEALBOT_FIX_MODE", "copilot_issue")
	t.Setenv("HEALBOT_FIX_AGENT", "claude")
	t.Setenv("HEALBOT_FIX_ASSIGNEE", "copilot-swe-agent[bot]")
	t.Setenv("HEALBOT_FIX_WAIT_CI", "true")
	cfg, err := Load(filepath.Join("..", "..", "config.demo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	mode, err := NormalizeFixMode(cfg.Fix.Mode)
	if err != nil || mode != FixModeIssue {
		t.Fatalf("mode %s err %v", cfg.Fix.Mode, err)
	}
	if cfg.Fix.Issue.Agent != FixAgentClaude || !cfg.Fix.WaitForCI {
		t.Fatalf("%+v", cfg.Fix)
	}
	t.Setenv("HEALBOT_FIX_MODE", "ship-it")
	if _, err := Load(filepath.Join("..", "..", "config.demo.yaml")); err == nil {
		t.Fatal("invalid fix mode was accepted")
	}
}
