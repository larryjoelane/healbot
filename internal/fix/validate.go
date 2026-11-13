package fix

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/larryjoelane/healbot/internal/config"
)

var errBadPath = fmt.Errorf("invalid path")

// defaultDeny is the path ceiling for model patches. A path is allowed only
// when it is listed in allow_denied_paths.
var defaultDeny = []string{
	".github/workflows/**",
	".github/actions/**",
	".github/hooks/**",
	"**/.github/workflows/**",
	"**/azure-pipelines.yml",
	"**/azure-pipelines.yaml",
	"**/.gitlab-ci.yml",
	"**/jenkinsfile",
	".circleci/**",
	"**/.env",
	"**/.env.*",
	"**/*.pem",
	"**/*.key",
	"**/*.pfx",
	"**/*.p12",
	"**/*secret*",
	"**/*secret*/**",
	"**/*credential*",
	"**/*credential*/**",
	"**/*id_rsa*",
	"**/*id_rsa*/**",
	"**/*.tf",
	"**/*.tfvars",
	"**/*.tfstate",
	"**/*.bicep",
	"**/*.bicepparam",
	"**/terraform/**",
	"terraform/**",
	"**/go.sum",
	"**/go.work.sum",
	"**/package-lock.json",
	"**/yarn.lock",
	"**/pnpm-lock.yaml",
	"**/bun.lock",
	"**/cargo.lock",
	"**/poetry.lock",
	"**/pipfile.lock",
	"**/composer.lock",
	"**/gemfile.lock",
	"**/packages.lock.json",
	"**/*.exe",
	"**/*.dll",
	"**/*.so",
	"**/*.dylib",
}

type sourceFile struct {
	Path      string
	Content   string
	Missing   bool
	Truncated bool
}

func validatePatch(files []File, sources map[string]sourceFile, cfg config.Fix) ([]File, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("model returned no files")
	}
	if len(files) > cfg.MaxFiles {
		return nil, fmt.Errorf("patch has %d files; limit is %d", len(files), cfg.MaxFiles)
	}
	seen := map[string]bool{}
	var out []File
	total := 0
	for _, f := range files {
		p, err := cleanPath(f.Path)
		if err != nil {
			return nil, fmt.Errorf("path %q is not a relative repository path", f.Path)
		}
		if seen[p] {
			return nil, fmt.Errorf("duplicate path %s", p)
		}
		seen[p] = true
		if !pathAllowed(p, cfg) {
			return nil, fmt.Errorf("path %s is not allowed", p)
		}
		src, known := sources[p]
		if known && src.Truncated {
			return nil, fmt.Errorf("path %s is too large to patch", p)
		}
		content := f.Content
		if strings.TrimSpace(content) == "" {
			return nil, fmt.Errorf("path %s is empty", p)
		}
		if !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
			return nil, fmt.Errorf("path %s is not valid text", p)
		}
		if looksLikeDiff(content) {
			return nil, fmt.Errorf("path %s is a diff; send the full file", p)
		}
		if content != redact(content) {
			return nil, fmt.Errorf("path %s contains a secret-like token", p)
		}
		if len(content) > cfg.MaxFileBytes {
			return nil, fmt.Errorf("path %s is %d bytes; limit is %d", p, len(content), cfg.MaxFileBytes)
		}
		total += len(content)
		if total > cfg.MaxPatchBytes {
			return nil, fmt.Errorf("patch is %d bytes; limit is %d", total, cfg.MaxPatchBytes)
		}
		file := File{Path: p, Content: content}
		if known && !src.Missing {
			if src.Content == content {
				continue
			}
			file.Update = true
		}
		out = append(out, file)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("patch does not change any file")
	}
	if !hasTest(out) {
		return nil, fmt.Errorf("patch has no regression test")
	}
	return out, nil
}

func pathAllowed(p string, cfg config.Fix) bool {
	if matchAny(cfg.AllowDeniedPaths, p) {
		if len(cfg.AllowPaths) == 0 || matchAny(cfg.AllowPaths, p) {
			return true
		}
		return false
	}
	if matchAny(defaultDeny, p) || matchAny(cfg.ExtraDenyPaths, p) {
		return false
	}
	if len(cfg.AllowPaths) == 0 {
		return true
	}
	return matchAny(cfg.AllowPaths, p)
}

func hasTest(files []File) bool {
	for _, f := range files {
		if isTestPath(f.Path) && strings.TrimSpace(f.Content) != "" {
			return true
		}
	}
	return false
}

func looksLikeDiff(s string) bool {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "diff --git") || strings.HasPrefix(t, "@@") {
		return true
	}
	return strings.HasPrefix(t, "--- ") && strings.Contains(t, "\n+++ ")
}
