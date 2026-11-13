package app

import (
	"testing"

	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/fix"
)

func TestDemoBuildWiresOfflineFix(t *testing.T) {
	cfg := config.Demo()
	cfg.Audit.Path = ""
	application, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Close)
	named, ok := application.Pipe.Fix.Model.(interface{ Name() string })
	if !ok || named.Name() != "demo" {
		t.Fatalf("demo model = %#v", application.Pipe.Fix.Model)
	}
	if _, ok := application.Pipe.Fix.Provider.(*fix.Memory); !ok {
		t.Fatalf("provider = %T", application.Pipe.Fix.Provider)
	}
	if application.Pipe.Fix.Issues == nil || application.Pipe.Fix.Source == nil {
		t.Fatal("demo provider was not wired as the issue and source client")
	}
	if application.Pipe.Fix.Fix.Mode != config.FixModeTemplate {
		t.Fatal(application.Pipe.Fix.Fix.Mode)
	}
}
