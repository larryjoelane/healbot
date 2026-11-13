package fix

import (
	"strings"
	"testing"

	"github.com/larryjoelane/healbot/internal/config"
)

func testFix() config.Fix {
	return config.Fix{Mode: config.FixModeModelPatch}.Normalized()
}

func TestValidateRejectsDeniedAndUnsafePatches(t *testing.T) {
	cfg := testFix()
	cases := []struct {
		name string
		file File
		cfg  config.Fix
		want string
	}{
		{"workflow", File{Path: ".github/workflows/ci.yml", Content: "name: ci\n"}, cfg, "not allowed"},
		{"lockfile", File{Path: "go.sum", Content: "module h1\n"}, cfg, "not allowed"},
		{"infra", File{Path: "deploy/main.tf", Content: "resource \"azurerm_resource_group\" \"g\" {}\n"}, cfg, "not allowed"},
		{"secret", File{Path: ".env", Content: "A=b\n"}, cfg, "not allowed"},
		{"traversal", File{Path: "../secret.txt", Content: "x\n"}, cfg, "relative"},
		{"diff", File{Path: "src/app.go", Content: "diff --git a/src/app.go b/src/app.go\n"}, cfg, "diff"},
		{"huge", File{Path: "src/app.go", Content: strings.Repeat("a", 50)}, config.Fix{MaxFiles: 2, MaxFileBytes: 20, MaxPatchBytes: 100}, "limit"},
		{"token", File{Path: "src/app.go", Content: "token := \"ghp_" + strings.Repeat("a", 36) + "\"\n"}, cfg, "secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := []File{tc.file, {Path: "tests/app_test.go", Content: "package app\n"}}
			if tc.name == "huge" {
				files = []File{tc.file}
			}
			_, err := validatePatch(files, nil, tc.cfg.Normalized())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestValidateAllowsDeniedPathWhenConfigured(t *testing.T) {
	cfg := testFix()
	cfg.AllowDeniedPaths = []string{"go.sum"}
	files, err := validatePatch([]File{
		{Path: "go.sum", Content: "example.com/mod v1.0.0 h1:abc\n"},
		{Path: "tests/app_test.go", Content: "package app\nfunc TestGuard(t *testing.T) {}\n"},
	}, nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("%+v", files)
	}
}

func TestValidateRequiresATestAndARealChange(t *testing.T) {
	cfg := testFix()
	if _, err := validatePatch([]File{{Path: "src/app.go", Content: "package app\n"}}, nil, cfg); err == nil {
		t.Fatal("expected a missing test")
	}
	_, err := validatePatch([]File{
		{Path: "src/app.go", Content: "package app\n"},
		{Path: "src/app_test.go", Content: "package app\n"},
	}, map[string]sourceFile{
		"src/app.go":      {Path: "src/app.go", Content: "package app\n"},
		"src/app_test.go": {Path: "src/app_test.go", Content: "package app\n"},
	}, cfg)
	if err == nil || !strings.Contains(err.Error(), "does not change") {
		t.Fatal(err)
	}
}

func TestValidateRejectsAllowlistMiss(t *testing.T) {
	cfg := testFix()
	cfg.AllowPaths = []string{"src/**", "tests/**"}
	_, err := validatePatch([]File{
		{Path: "README.md", Content: "hi\n"},
		{Path: "tests/app_test.go", Content: "package app\n"},
	}, nil, cfg)
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatal(err)
	}
}
