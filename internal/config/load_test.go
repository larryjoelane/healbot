package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFile(t *testing.T) {
	t.Setenv("HEALBOT_CONFIG", "")
	if ConfigFile("/config.demo.yaml") != "/config.demo.yaml" {
		t.Fatalf("flag path %q", ConfigFile("/config.demo.yaml"))
	}
	if ConfigFile("") != "config.demo.yaml" {
		t.Fatalf("empty flag %q", ConfigFile(""))
	}
	t.Setenv("HEALBOT_CONFIG", "/config.azure.yaml")
	if ConfigFile("/config.demo.yaml") != "/config.azure.yaml" {
		t.Fatalf("env override %q", ConfigFile("/config.demo.yaml"))
	}
}

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
