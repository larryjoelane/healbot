package fix

import (
	"path"
	"regexp"
	"strings"

	"github.com/larryjoelane/healbot/internal/model"
)

var stackPathRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)File "([^"]+)"`),
	regexp.MustCompile(`(?i)(?:^|[\s("'\[])((?:[A-Za-z]:)?(?:[\w.-]+[/\\])+[\w.-]+\.[A-Za-z0-9]{1,8})`),
	regexp.MustCompile(`(?i)(?:^|[\s("'\[])((?:[\w.-]+[/\\])*[\w.-]+\.(?:cs|py|go|java|js|ts|tsx|jsx|mjs|cjs|rb|php|kt|scala|rs|fs))`),
}

// extractPaths returns repo-relative guesses for files named by the stack.
// Each inner slice is ordered from most specific to the file name alone.
func extractPaths(inc model.Incident) [][]string {
	text := inc.StackTrace + "\n" + inc.TopFrame
	seen := map[string]bool{}
	var out [][]string
	for _, re := range stackPathRes {
		for _, m := range re.FindAllStringSubmatch(text, 32) {
			raw := ""
			for i := 1; i < len(m); i++ {
				if m[i] != "" {
					raw = m[i]
					break
				}
			}
			cands := pathCandidates(raw)
			if len(cands) == 0 {
				continue
			}
			key := strings.Join(cands, "|")
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, cands)
		}
	}
	return out
}

func pathCandidates(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"'`)
	raw = strings.ReplaceAll(raw, "\\", "/")
	if raw == "" {
		return nil
	}
	if len(raw) >= 2 && raw[1] == ':' {
		raw = raw[2:]
	}
	raw = strings.TrimPrefix(raw, "/")
	var out []string
	add := func(p string) {
		p = strings.TrimPrefix(p, "/")
		if _, err := cleanPath(p); err != nil {
			return
		}
		for _, e := range out {
			if e == p {
				return
			}
		}
		out = append(out, p)
	}
	add(raw)
	parts := strings.Split(raw, "/")
	for i, part := range parts {
		switch strings.ToLower(part) {
		case "src", "app", "lib", "pkg", "internal", "tests", "test", "source":
			add(strings.Join(parts[i:], "/"))
		}
	}
	if len(parts) > 0 {
		add(parts[len(parts)-1])
	}
	return out
}

func cleanPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsRune(p, 0) {
		return "", errBadPath
	}
	if len(p) >= 2 && p[1] == ':' {
		return "", errBadPath
	}
	if p == ".git" || strings.HasPrefix(p, ".git/") {
		return "", errBadPath
	}
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." || s == ".." {
			return "", errBadPath
		}
	}
	return p, nil
}

func matchAny(patterns []string, p string) bool {
	p = strings.ToLower(p)
	for _, pattern := range patterns {
		if matchGlob(strings.ToLower(pattern), p) {
			return true
		}
	}
	return false
}

func matchGlob(pattern, p string) bool {
	return globMatch(splitPath(pattern), splitPath(p))
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func globMatch(pat, segs []string) bool {
	for {
		if len(pat) == 0 {
			return len(segs) == 0
		}
		if pat[0] == "**" {
			if globMatch(pat[1:], segs) {
				return true
			}
			if len(segs) == 0 {
				return false
			}
			segs = segs[1:]
			continue
		}
		if len(segs) == 0 || !matchSeg(pat[0], segs[0]) {
			return false
		}
		pat = pat[1:]
		segs = segs[1:]
	}
}

func matchSeg(pattern, seg string) bool {
	ok, err := path.Match(pattern, seg)
	return err == nil && ok
}

func isTestPath(p string) bool {
	low := strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
	base := path.Base(low)
	if strings.HasPrefix(low, "tests/") || strings.HasPrefix(low, "test/") || strings.HasPrefix(low, "spec/") ||
		strings.Contains(low, "/tests/") || strings.Contains(low, "/test/") || strings.Contains(low, "/spec/") ||
		strings.Contains(low, "/__tests__/") {
		return true
	}
	switch {
	case strings.HasPrefix(base, "test_"), strings.HasPrefix(base, "test."):
		return true
	case strings.HasSuffix(base, "_test.go"), strings.HasSuffix(base, "_test.py"), strings.HasSuffix(base, "_spec.rb"):
		return true
	case strings.Contains(base, ".test."), strings.Contains(base, ".spec."), strings.Contains(base, "_test."), strings.Contains(base, "_spec."):
		return true
	case strings.HasSuffix(base, "tests.cs"), strings.HasSuffix(base, "test.cs"), strings.HasSuffix(base, "tests.java"), strings.HasSuffix(base, "test.java"):
		return true
	default:
		return false
	}
}
