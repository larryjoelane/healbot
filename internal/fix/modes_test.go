package fix

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/model"
)

func sampleIncident() model.Incident {
	return model.Incident{
		ID:            "inc_1",
		ResourceID:    "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/example-rg/providers/Microsoft.Web/sites/example-api",
		Severity:      model.Sev2,
		ExceptionType: "System.NullReferenceException",
		TopFrame:      "OrderService.Place",
		Operation:     "POST /v1/orders",
		Message:       "Object reference not set",
		StackTrace:    "at Example.Orders.OrderService.Place(OrderService.cs:88)",
		Hypothesis: &model.Hypothesis{
			Summary: "null customer", Confidence: 0.9, Category: model.CategoryCode, LikelyCode: true, Provider: "heuristic",
			Evidence: []string{"slot swap 12 minutes ago"},
		},
	}
}

type captureModel struct {
	user string
	raw  string
	err  error
}

func (m *captureModel) Complete(_ context.Context, _, user string) (string, error) {
	m.user = user
	if m.err != nil {
		return "", m.err
	}
	if m.raw != "" {
		return m.raw, nil
	}
	return DemoModel{}.Complete(context.Background(), "", user)
}

func TestModeSelectionAndResourceOverride(t *testing.T) {
	inc := sampleIncident()
	mem := NewMemory()
	mem.AddFile("AGENTS.md", "# Architecture\nKeep the handler thin.\n")
	mem.AddFile("OrderService.cs", "class OrderService {}\n")
	svc := Service{
		Provider: mem, Source: mem, Issues: mem, CI: mem, Model: DemoModel{},
		Fix: config.Fix{Mode: config.FixModeTemplate},
	}
	template, err := svc.Open(context.Background(), inc, "dry_run", config.Resource{})
	if err != nil || template.Mode != config.FixModeTemplate || template.Ref.ID == "" {
		t.Fatalf("%+v %v", template, err)
	}
	if len(mem.issues) != 0 {
		t.Fatal("template mode opened an issue")
	}

	patched, err := svc.Open(context.Background(), inc, "dry_run", config.Resource{FixMode: config.FixModeModelPatch})
	if err != nil || patched.Mode != config.FixModeModelPatch || patched.Requested != config.FixModeModelPatch {
		t.Fatalf("%+v %v", patched, err)
	}
	pr, ok := mem.Get(patched.Ref.ID)
	if !ok || !hasTest(pr.Files) {
		t.Fatalf("%+v", pr)
	}

	issued, err := svc.Open(context.Background(), inc, "dry_run", config.Resource{FixMode: "copilot_issue"})
	if err != nil || issued.Mode != config.FixModeIssue || issued.Issue == nil {
		t.Fatalf("%+v %v", issued, err)
	}
	if issued.Issue.Agent != config.FixAgentCopilot || !strings.Contains(issued.Issue.Assignee, "copilot-swe-agent") {
		t.Fatalf("%+v", issued.Issue)
	}
	rec, ok := mem.GetIssue(issued.Issue.ID)
	if !ok || !strings.Contains(rec.Body, "AGENTS.md") || !strings.Contains(rec.Body, "does not") && !strings.Contains(rec.Body, "Do not merge") {
		t.Fatalf("%s", rec.Body)
	}
}

func TestDemoModesStayOffline(t *testing.T) {
	inc := sampleIncident()
	for _, mode := range []string{config.FixModeTemplate, config.FixModeModelPatch, "copilot_issue", config.FixModeIssue} {
		mem := NewMemory()
		mem.AddFile("CLAUDE.md", "@AGENTS.md\n")
		mem.AddFile(".claude/skills/incident-fix/SKILL.md", "---\nname: incident-fix\n---\nFollow AGENTS.md\n")
		mem.AddFile("AGENTS.md", "Canonical architecture guide.\n")
		svc := Service{Provider: mem, Source: mem, Issues: mem, CI: mem, Model: DemoModel{}, Fix: config.Fix{Mode: mode}}
		out, err := svc.Open(context.Background(), inc, "dry_run", config.Resource{})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if out.Ref.ID == "" && out.Issue == nil {
			t.Fatalf("%s produced nothing", mode)
		}
	}
}

func TestModelFailureFallsBackToTemplate(t *testing.T) {
	mem := NewMemory()
	secret := "ghp_" + strings.Repeat("z", 36)
	inc := sampleIncident()
	inc.StackTrace = "token " + secret + " at OrderService.cs:88"
	svc := Service{
		Provider: mem, Source: mem, Model: &captureModel{err: errModel("upstream unavailable")},
		Fix: config.Fix{Mode: config.FixModeModelPatch},
	}
	out, err := svc.Open(context.Background(), inc, "dry_run", config.Resource{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Mode != config.FixModeTemplate || out.Requested != config.FixModeModelPatch || !strings.Contains(out.Fallback, "upstream") {
		t.Fatalf("%+v", out)
	}
	pr, ok := mem.Get(out.Ref.ID)
	if !ok || !strings.Contains(pr.Body, "Mode fallback") || strings.Contains(pr.Body, secret) {
		t.Fatal(pr.Body)
	}
	var sawTemplate bool
	for _, f := range pr.Files {
		if strings.Contains(f.Path, "OrderService.cs") {
			sawTemplate = true
		}
	}
	if !sawTemplate {
		t.Fatalf("%+v", pr.Files)
	}
}

func TestPromptRedactsAndIncludesInstructions(t *testing.T) {
	mem := NewMemory()
	mem.AddFile("AGENTS.md", "Keep handlers thin and put the rule in the service.\n")
	mem.AddFile("CLAUDE.md", "@AGENTS.md\n")
	mem.AddFile(".claude/skills/incident-fix/SKILL.md", "---\nname: incident-fix\ndescription: Fix an incident\n---\nFollow AGENTS.md\n")
	mem.AddFile(".github/copilot-instructions.md", "Follow AGENTS.md\n")
	secret := strings.Repeat("A", 44)
	inc := sampleIncident()
	inc.StackTrace = "Account" + "Key=" + secret + "\nat OrderService.cs:88"
	model := &captureModel{}
	svc := Service{Provider: mem, Source: mem, Model: model, Fix: config.Fix{Mode: config.FixModeModelPatch}}
	out, err := svc.Open(context.Background(), inc, "dry_run", config.Resource{})
	if err != nil || out.Mode != config.FixModeModelPatch {
		t.Fatalf("%+v %v", out, err)
	}
	if strings.Contains(model.user, secret) {
		t.Fatal("prompt contained the connection-string secret")
	}
	if !strings.Contains(model.user, "[REDACTED]") {
		t.Fatal(model.user)
	}
	for _, needle := range []string{"INSTRUCTION path=AGENTS.md", "Keep handlers thin", "INSTRUCTION path=CLAUDE.md", ".claude/skills/incident-fix/SKILL.md"} {
		if !strings.Contains(model.user, needle) {
			t.Fatalf("prompt missing %s\n%s", needle, model.user)
		}
	}
}

func TestValidationFailureFallsBack(t *testing.T) {
	mem := NewMemory()
	raw, _ := json.Marshal(patchDoc{Title: "bad", Files: []patchFile{{
		Path: ".github/workflows/ci.yml", Content: "name: ci\n",
	}}})
	svc := Service{
		Provider: mem, Model: &captureModel{raw: string(raw)},
		Fix: config.Fix{Mode: config.FixModeModelPatch},
	}
	out, err := svc.Open(context.Background(), sampleIncident(), "dry_run", config.Resource{})
	if err != nil || out.Fallback == "" || out.Mode != config.FixModeTemplate {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestClaudeIssueMentionsAndDoesNotAssignCopilot(t *testing.T) {
	mem := NewMemory()
	mem.AddFile("AGENTS.md", "Canonical.\n")
	mem.AddFile(".claude/agents/incident-fixer.md", "---\nname: incident-fixer\n---\nFollow AGENTS.md\n")
	svc := Service{
		Provider: mem, Source: mem, Issues: mem,
		Fix: config.Fix{Mode: config.FixModeIssue, Issue: config.FixIssue{Agent: config.FixAgentClaude, Labels: []string{"claude"}}},
	}
	out, err := svc.Open(context.Background(), sampleIncident(), "dry_run", config.Resource{})
	if err != nil || out.Issue == nil {
		t.Fatalf("%+v %v", out, err)
	}
	rec, _ := mem.GetIssue(out.Issue.ID)
	if !strings.Contains(rec.Body, "@claude") || !strings.Contains(rec.Comment, "@claude") {
		t.Fatalf("body=%s\ncomment=%s", rec.Body, rec.Comment)
	}
	if strings.Contains(rec.Body, "copilot-swe-agent") {
		t.Fatal(rec.Body)
	}
	if len(rec.Labels) != 1 || rec.Labels[0] != "claude" {
		t.Fatal(rec.Labels)
	}
	if len(mem.List()) != 0 {
		t.Fatal("issue mode opened a pull request")
	}
}

func TestIssueModeWithoutGitHubFallsBack(t *testing.T) {
	mem := NewMemory()
	svc := Service{Provider: mem, Fix: config.Fix{Mode: config.FixModeIssue}}
	out, err := svc.Open(context.Background(), sampleIncident(), "dry_run", config.Resource{})
	if err != nil || !strings.Contains(out.Fallback, "GitHub") || out.Ref.ID == "" {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestCIFailureIsLabeledAndNotMerged(t *testing.T) {
	mem := NewMemory()
	mem.CIState = "failure"
	svc := Service{
		Provider: mem, CI: mem,
		Fix: config.Fix{Mode: config.FixModeTemplate, WaitForCI: true},
	}
	out, err := svc.Open(context.Background(), sampleIncident(), "dry_run", config.Resource{})
	if err != nil || out.Ref.CI != "failure" {
		t.Fatalf("%+v %v", out, err)
	}
	pr, _ := mem.Get(out.Ref.ID)
	if len(pr.Labels) != 1 || pr.Labels[0] != "healbot:ci-failed" {
		t.Fatal(pr.Labels)
	}
}

func TestGitHubModelPatchUpdatesExistingFile(t *testing.T) {
	var paths []string
	var sawSHA bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if strings.Contains(strings.ToLower(r.URL.Path), "merge") {
			t.Errorf("merge endpoint: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/contents/src/app.go") && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"type":"file","encoding":"base64","content":"cGFja2FnZSBhcHAK","sha":"blobsha"}`)
		case strings.Contains(r.URL.Path, "/contents/") && r.Method == http.MethodGet:
			http.NotFound(w, r)
		case r.URL.Path == "/repos/example-org/api" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"default_branch":"main"}`)
		case strings.Contains(r.URL.Path, "/git/ref/heads/") && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"object":{"sha":"abc123"}}`)
		case strings.HasSuffix(r.URL.Path, "/git/refs") && r.Method == http.MethodPost:
			_, _ = io.WriteString(w, `{}`)
		case strings.Contains(r.URL.Path, "/contents/") && r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if strings.Contains(r.URL.Path, "src/app.go") {
				if body["sha"] != "blobsha" {
					t.Fatalf("update missing sha: %+v", body)
				}
				sawSHA = true
			}
			_, _ = io.WriteString(w, `{"commit":{"sha":"newsha"},"content":{"path":"x"}}`)
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == http.MethodPost:
			_, _ = io.WriteString(w, `{"number":9,"html_url":"https://github.com/example-org/api/pull/9"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	raw, _ := json.Marshal(patchDoc{
		Title: "fix: guard",
		Files: []patchFile{
			{Path: "src/app.go", Content: "package app\nfunc Guard() {}\n"},
			{Path: "src/app_test.go", Content: "package app\nfunc TestGuard(t *testing.T) {}\n"},
		},
	})
	gh := &GitHub{BaseURL: srv.URL, Owner: "example-org", Repo: "api", Token: "test-token", HTTP: srv.Client()}
	svc := Service{Provider: gh, Source: gh, Model: &captureModel{raw: string(raw)}, Fix: config.Fix{Mode: config.FixModeModelPatch}}
	out, err := svc.Open(context.Background(), model.Incident{
		ID: "inc_9", ExceptionType: "panic", TopFrame: "Guard", StackTrace: "src/app.go:10",
	}, "dry_run", config.Resource{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Mode != config.FixModeModelPatch || out.Ref.ID != "9" || out.Ref.SHA != "newsha" || !sawSHA {
		t.Fatalf("%+v sha=%v", out, sawSHA)
	}
	joined := strings.Join(paths, "\n")
	if strings.Contains(strings.ToLower(joined), "merge") {
		t.Fatal(joined)
	}
}

func TestGitHubAssignsCopilotAndFallsBackToGraphQL(t *testing.T) {
	var calls []string
	var features string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if strings.Contains(strings.ToLower(r.URL.Path), "merge") {
			t.Errorf("merge endpoint: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/example-org/api/issues" && r.Method == http.MethodPost:
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			if _, ok := req["agent_assignment"]; !ok {
				t.Fatalf("missing agent_assignment: %s", body)
			}
			assignees, _ := req["assignees"].([]any)
			if len(assignees) != 1 || assignees[0] != "copilot-swe-agent[bot]" {
				t.Fatalf("assignees %+v", assignees)
			}
			_, _ = io.WriteString(w, `{"number":4,"node_id":"ISSUE_NODE","html_url":"https://github.com/example-org/api/issues/4","assignees":[]}`)
		case strings.HasSuffix(r.URL.Path, "/assignees") && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, `{"message":"cannot assign"}`)
		case r.URL.Path == "/graphql" && r.Method == http.MethodPost:
			features = r.Header.Get("GraphQL-Features")
			var req struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal(body, &req)
			if strings.Contains(req.Query, "suggestedActors") {
				_, _ = io.WriteString(w, `{"data":{"repository":{"id":"REPO_NODE","issue":{"id":"ISSUE_NODE"},"suggestedActors":{"nodes":[{"login":"copilot-swe-agent","id":"BOT_NODE"}]}}}}`)
				return
			}
			if !strings.Contains(req.Query, "replaceActorsForAssignable") {
				t.Fatalf("query %s", req.Query)
			}
			_, _ = io.WriteString(w, `{"data":{"replaceActorsForAssignable":{"assignable":{"number":4}}}}`)
		case r.URL.Path == "/repos/example-org/api" && r.Method == http.MethodGet:
			_, _ = io.WriteString(w, `{"default_branch":"main"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	gh := &GitHub{BaseURL: srv.URL, Owner: "example-org", Repo: "api", Token: "test-token", HTTP: srv.Client()}
	svc := Service{Provider: gh, Issues: gh, Fix: config.Fix{Mode: "copilot_issue"}}
	out, err := svc.Open(context.Background(), sampleIncident(), "dry_run", config.Resource{})
	if err != nil || out.Warning != "" || out.Issue == nil || out.Issue.ID != "4" {
		t.Fatalf("%+v %v", out, err)
	}
	if !strings.Contains(features, "coding_agent_model_selection") {
		t.Fatal(features)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "POST /graphql") || strings.Contains(strings.ToLower(joined), "merge") {
		t.Fatal(joined)
	}
}

func TestGitHubClaudeComment(t *testing.T) {
	var comment string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.URL.Path), "merge") {
			t.Errorf("merge endpoint: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/issues") && r.Method == http.MethodPost:
			var req map[string]any
			_ = json.Unmarshal(body, &req)
			if _, ok := req["agent_assignment"]; ok {
				t.Fatal("claude issue included copilot assignment")
			}
			if !strings.Contains(req["body"].(string), "@claude") {
				t.Fatalf("%s", req["body"])
			}
			_, _ = io.WriteString(w, `{"number":8,"html_url":"https://github.com/example-org/api/issues/8"}`)
		case strings.Contains(r.URL.Path, "/comments") && r.Method == http.MethodPost:
			var req map[string]string
			_ = json.Unmarshal(body, &req)
			comment = req["body"]
			_, _ = io.WriteString(w, `{"id":1}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	gh := &GitHub{BaseURL: srv.URL, Owner: "example-org", Repo: "api", Token: "test-token", HTTP: srv.Client()}
	svc := Service{Issues: gh, Fix: config.Fix{Mode: config.FixModeIssue, Issue: config.FixIssue{Agent: config.FixAgentClaude}}}
	out, err := svc.Open(context.Background(), sampleIncident(), "dry_run", config.Resource{})
	if err != nil || out.Issue == nil || out.Issue.URL == "" {
		t.Fatalf("%+v %v", out, err)
	}
	if !strings.Contains(comment, "@claude") {
		t.Fatal(comment)
	}
}

func TestPackageHasNoMergeAPI(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	var saw bool
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		saw = true
		file, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				if strings.Contains(strings.ToLower(node.Name.Name), "merge") {
					t.Errorf("merge function %s", node.Name.Name)
				}
			case *ast.BasicLit:
				if node.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(node.Value)
				if err != nil {
					return true
				}
				low := strings.ToLower(value)
				if strings.Contains(low, "/merge") || strings.Contains(low, "/merges") {
					t.Errorf("merge endpoint literal %s", value)
				}
			}
			return true
		})
	}
	if !saw {
		t.Fatal("no source files parsed")
	}
}

type errModel string

func (e errModel) Error() string { return string(e) }
