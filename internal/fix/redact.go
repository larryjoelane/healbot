package fix

import (
	"regexp"
	"strings"
)

// redact strips obvious tokens and connection strings before text is sent to
// a model or written into a pull request or issue.
func redact(s string) string {
	if s == "" {
		return s
	}
	for _, re := range redactPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

var redactPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)DefaultEndpointsProtocol=[^\s"]+`),
	regexp.MustCompile(`(?i)AccountKey=[^;\s"]+`),
	regexp.MustCompile(`(?i)SharedAccessSignature=[^;\s"]+`),
	regexp.MustCompile(`(?i)\b(?:password|pwd)\s*=\s*[^;\s"]+`),
	regexp.MustCompile(`(?i)"(?:api[_-]?key|client[_-]?secret|access[_-]?token|password)"\s*:\s*"[^"]+"`),
	regexp.MustCompile(`(?i)\b(?:api[_-]?key|client[_-]?secret|access[_-]?token)\s*[:=]\s*[A-Za-z0-9+/=_\-]{16,}`),
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*`),
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`glpat-[A-Za-z0-9\-_]{20,}`),
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`),
}

func boundText(s string, n int) string {
	s = redact(s)
	if n < 1 || len(s) <= n {
		return s
	}
	return s[:n] + "\n[truncated]\n"
}

func oneLine(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	s = strings.Join(strings.Fields(s), " ")
	if n > 0 && len(s) > n {
		return s[:n]
	}
	return s
}
