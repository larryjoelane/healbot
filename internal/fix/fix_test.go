package fix

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/larryjoelane/healbot/internal/model"
)

func TestProposeIncludesTestAndNoMergeLanguage(t *testing.T) {
	draft := Propose(model.Incident{
		ID:            "inc_1",
		ResourceID:    "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Web/sites/example-api",
		ExceptionType: "System.NullReferenceException",
		TopFrame:      "OrderService.Place",
		StackTrace:    "at Example.Orders.OrderService.Place(OrderService.cs:88)",
		Hypothesis: &model.Hypothesis{
			Summary: "null customer", Confidence: 0.9, Category: model.CategoryCode, LikelyCode: true, Provider: "heuristic",
		},
	}, "dry_run")
	if !strings.Contains(draft.Body, "does not merge") {
		t.Fatal(draft.Body)
	}
	var sawTest bool
	for _, f := range draft.Files {
		if strings.Contains(f.Path, "Tests") && strings.Contains(f.Content, "Fact") {
			sawTest = true
		}
	}
	if !sawTest {
		t.Fatal("expected a regression test")
	}
}

func TestGitHubNeverMerges(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if strings.Contains(strings.ToLower(r.URL.Path), "merge") {
			t.Errorf("merge endpoint called: %s", r.URL.Path)
			http.Error(w, "no", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/repos/example-org/api" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"default_branch":"main"}`))
		case strings.Contains(r.URL.Path, "/git/ref/heads/") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"object":{"sha":"abc123"}}`))
		case strings.HasSuffix(r.URL.Path, "/git/refs") && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{}`))
		case strings.Contains(r.URL.Path, "/contents/") && r.Method == http.MethodPut:
			_, _ = w.Write([]byte(`{"content":{"path":"x"}}`))
		case strings.HasSuffix(r.URL.Path, "/pulls") && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var req map[string]string
			_ = json.Unmarshal(body, &req)
			if req["title"] == "" || req["head"] == "" || req["base"] != "main" {
				t.Fatalf("bad pr body %s", body)
			}
			_, _ = w.Write([]byte(`{"number":7,"html_url":"https://github.com/example-org/api/pull/7","title":"` + req["title"] + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	gh := &GitHub{BaseURL: srv.URL, Owner: "example-org", Repo: "api", Token: "t", HTTP: srv.Client()}
	ref, err := gh.Create(context.Background(), Propose(model.Incident{
		ID: "inc_9", ExceptionType: "System.NullReferenceException", TopFrame: "OrderService.Place",
	}, "dry_run"))
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "7" || ref.Provider != "github" {
		t.Fatalf("%+v", ref)
	}
	joined := strings.Join(paths, "\n")
	if !strings.Contains(joined, "POST /repos/example-org/api/pulls") {
		t.Fatal(joined)
	}
}

func TestAzureDevOpsOpensPR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.URL.Path), "merge") || strings.Contains(r.URL.RawQuery, "merge") {
			t.Errorf("merge called: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/repositories/api") && !strings.Contains(r.URL.Path, "/refs") && !strings.Contains(r.URL.Path, "/pushes") && !strings.Contains(r.URL.Path, "/pullrequests") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"repo","defaultBranch":"refs/heads/main"}`))
		case strings.Contains(r.URL.Path, "/refs"):
			_, _ = w.Write([]byte(`{"value":[{"name":"refs/heads/main","objectId":"deadbeef"}]}`))
		case strings.Contains(r.URL.Path, "/pushes"):
			_, _ = w.Write([]byte(`{"pushId":1}`))
		case strings.Contains(r.URL.Path, "/pullrequests"):
			_, _ = w.Write([]byte(`{"pullRequestId":15,"url":"https://dev.azure.com/org/proj/_git/api/pullrequest/15"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := &AzureDevOps{BaseURL: srv.URL, Org: "org", Project: "proj", Repo: "api", Token: "pat", HTTP: srv.Client()}
	ref, err := client.Create(context.Background(), Draft{
		Title: "fix", Body: "body", Branch: "healbot/inc_1",
		Files: []File{{Path: "a.cs", Content: "class A {}"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "15" || ref.Provider != "azure_devops" {
		t.Fatalf("%+v", ref)
	}
}

func TestAzureDevOpsReadsFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(strings.ToLower(r.URL.Path), "merge") {
			t.Errorf("merge called: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.RawQuery, "scopePath") {
			if strings.Contains(r.URL.RawQuery, "incident-fix") {
				_, _ = io.WriteString(w, `{"value":[]}`)
				return
			}
			_, _ = io.WriteString(w, `{"value":[{"path":"/src/app.go","isFolder":false},{"path":"/.claude/skills/incident-fix","isFolder":true}]}`)
			return
		}
		if strings.Contains(r.URL.RawQuery, "path=") {
			_, _ = io.WriteString(w, `{"path":"/src/app.go","content":"package app\n"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	client := &AzureDevOps{BaseURL: srv.URL, Org: "example-org", Project: "example", Repo: "api", Token: "test-token", HTTP: srv.Client()}
	body, missing, err := client.Read(context.Background(), "src/app.go", "")
	if err != nil || missing || !strings.Contains(body, "package app") {
		t.Fatalf("body=%q missing=%v err=%v", body, missing, err)
	}
	files, err := client.ListFiles(context.Background(), ".claude/skills", "")
	if err != nil || len(files) != 1 || files[0] != "src/app.go" {
		t.Fatalf("%v %v", files, err)
	}
}
