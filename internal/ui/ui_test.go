package ui

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/larryjoelane/healbot/internal/fix"
	"github.com/larryjoelane/healbot/internal/model"
)

func TestIncidentPageShowsUnmergedPullAndIssue(t *testing.T) {
	rec := httptest.NewRecorder()
	Render(rec, Page{
		Title: "incident",
		View:  "incident",
		Incident: model.Incident{
			ID:     "inc_1",
			Title:  "NullReferenceException in OrderService.Place",
			Status: model.StatusIssueOpened,
			PullRequest: &model.PullRequestRef{
				Title: "fix: guard", URL: "/pulls/pr_1", Provider: "mock", Branch: "healbot/inc_1", Mode: "template",
			},
			Issue: &model.IssueRef{
				Title: "fix: null customer", URL: "/issues/issue_1", Agent: "copilot", Assignee: "copilot-swe-agent[bot]",
			},
		},
		Pull: fix.Pull{},
	})
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
	for _, needle := range []string{"not merged", "issue only, not merged", "copilot-swe-agent[bot]", "/pulls/pr_1", "/issues/issue_1"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("missing %s\n%s", needle, body)
		}
	}
}
