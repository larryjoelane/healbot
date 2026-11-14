package fix

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// DemoModel is the offline patch writer used in demo mode. It does not call
// a network. The patch is a deterministic stand-in so the model_patch path
// can be exercised without Azure OpenAI.
type DemoModel struct{}

func (DemoModel) Name() string { return "demo" }

var sourceLine = regexp.MustCompile(`(?m)^SOURCE path=(.+)$`)

func (DemoModel) Complete(ctx context.Context, _, user string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path := "src/healbot_fix.txt"
	if m := sourceLine.FindStringSubmatch(user); len(m) == 2 {
		got := strings.TrimSpace(m[1])
		if got != "" && !strings.Contains(got, "..") && !strings.HasPrefix(got, "/") {
			path = got
		}
	}
	doc := patchDoc{
		Title:   "fix: guard the failing path",
		Summary: "Demo model patch. This is a placeholder, not a language-specific repair.",
		Files: []patchFile{
			{Path: path, Content: "// healbot demo model patch\n// guard the failing path and reject the empty input\n"},
			{Path: "tests/healbot_fix_test.txt", Content: "regression: the failing input is rejected instead of throwing\n"},
		},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
