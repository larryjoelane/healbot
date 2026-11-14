package fix

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/larryjoelane/healbot/internal/config"
	"github.com/larryjoelane/healbot/internal/model"
)

func (g *GitHub) raw(ctx context.Context, method, path string, in any, headers map[string]string) (int, []byte, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	base := g.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "healbot")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	client := g.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, raw, nil
}

type ghContent struct {
	Type     string `json:"type"`
	Path     string `json:"path"`
	SHA      string `json:"sha"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

func (g *GitHub) Read(ctx context.Context, path, ref string) (string, bool, error) {
	u := g.repo() + "/contents/" + escapePath(path)
	if ref != "" {
		u += "?ref=" + url.QueryEscape(ref)
	}
	code, body, err := g.raw(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return "", false, err
	}
	if code == http.StatusNotFound {
		return "", true, nil
	}
	if code >= 300 {
		return "", false, fmt.Errorf("github contents %s: status %d", path, code)
	}
	var obj ghContent
	if err := json.Unmarshal(body, &obj); err != nil {
		return "", false, fmt.Errorf("github contents %s: %w", path, err)
	}
	if obj.Encoding == "base64" {
		clean := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' {
				return -1
			}
			return r
		}, obj.Content)
		decoded, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			return "", false, fmt.Errorf("github contents %s: %w", path, err)
		}
		return string(decoded), false, nil
	}
	return obj.Content, false, nil
}

func (g *GitHub) ListFiles(ctx context.Context, dir, ref string) ([]string, error) {
	var out []string
	if err := g.walk(ctx, strings.Trim(dir, "/"), ref, 0, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (g *GitHub) walk(ctx context.Context, dir, ref string, depth int, out *[]string) error {
	if depth > 4 || len(*out) >= 40 {
		return nil
	}
	u := g.repo() + "/contents/" + escapePath(dir)
	if ref != "" {
		u += "?ref=" + url.QueryEscape(ref)
	}
	code, body, err := g.raw(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return err
	}
	if code == http.StatusNotFound {
		return nil
	}
	if code >= 300 {
		return fmt.Errorf("github list %s: status %d", dir, code)
	}
	entries, err := decodeContents(body)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if len(*out) >= 40 {
			return nil
		}
		switch e.Type {
		case "dir":
			if err := g.walk(ctx, e.Path, ref, depth+1, out); err != nil {
				return err
			}
		case "file", "":
			if e.Path != "" {
				*out = append(*out, e.Path)
			}
		}
	}
	return nil
}

func decodeContents(body []byte) ([]ghContent, error) {
	trim := bytes.TrimSpace(body)
	if len(trim) == 0 {
		return nil, nil
	}
	if trim[0] == '[' {
		var list []ghContent
		if err := json.Unmarshal(trim, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	var one ghContent
	if err := json.Unmarshal(trim, &one); err != nil {
		return nil, err
	}
	if one.Type == "file" || one.Path != "" {
		return []ghContent{one}, nil
	}
	return nil, nil
}

func (g *GitHub) blobSHA(ctx context.Context, path, branch string) (string, bool, error) {
	u := g.repo() + "/contents/" + escapePath(path) + "?ref=" + url.QueryEscape(branch)
	code, body, err := g.raw(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return "", false, err
	}
	if code == http.StatusNotFound {
		return "", false, nil
	}
	if code >= 300 {
		return "", false, fmt.Errorf("github contents %s: status %d", path, code)
	}
	var obj ghContent
	if err := json.Unmarshal(body, &obj); err != nil {
		return "", false, err
	}
	if obj.SHA == "" {
		return "", false, nil
	}
	return obj.SHA, true, nil
}

func escapePath(p string) string {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

type ghNewIssue struct {
	Title           string      `json:"title"`
	Body            string      `json:"body"`
	Assignees       []string    `json:"assignees,omitempty"`
	Labels          []string    `json:"labels,omitempty"`
	AgentAssignment *ghAgentJob `json:"agent_assignment,omitempty"`
}

type ghAgentJob struct {
	TargetRepo         string `json:"target_repo"`
	BaseBranch         string `json:"base_branch"`
	CustomInstructions string `json:"custom_instructions"`
	CustomAgent        string `json:"custom_agent"`
	Model              string `json:"model"`
}

type ghIssue struct {
	Number    int    `json:"number"`
	HTMLURL   string `json:"html_url"`
	NodeID    string `json:"node_id"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

// CreateIssue opens an issue and, for the Copilot agent, assigns it.
// A failure after the issue exists returns the ref and the error.
func (g *GitHub) CreateIssue(ctx context.Context, draft IssueDraft) (model.IssueRef, error) {
	payload := ghNewIssue{Title: draft.Title, Body: draft.Body, Labels: draft.Labels}
	if draft.Agent == config.FixAgentCopilot && draft.Assignee != "" {
		base := draft.BaseBranch
		if base == "" {
			base = g.defaultBranch(ctx)
		}
		payload.Assignees = []string{draft.Assignee}
		payload.AgentAssignment = &ghAgentJob{
			TargetRepo:         g.Owner + "/" + g.Repo,
			BaseBranch:         base,
			CustomInstructions: draft.CustomInstructions,
			CustomAgent:        draft.CustomAgent,
		}
	}
	code, body, err := g.raw(ctx, http.MethodPost, g.repo()+"/issues", payload, nil)
	if err != nil {
		return model.IssueRef{}, err
	}
	if code == http.StatusUnprocessableEntity || code == http.StatusBadRequest {
		payload.Assignees = nil
		payload.AgentAssignment = nil
		payload.Labels = nil
		code, body, err = g.raw(ctx, http.MethodPost, g.repo()+"/issues", payload, nil)
		if err != nil {
			return model.IssueRef{}, err
		}
	}
	if code >= 300 {
		return model.IssueRef{}, fmt.Errorf("github issue: status %d", code)
	}
	var created ghIssue
	if err := json.Unmarshal(body, &created); err != nil {
		return model.IssueRef{}, fmt.Errorf("github issue decode: %w", err)
	}
	ref := issueRef(g, created, draft)
	var warn error
	if draft.Agent == config.FixAgentCopilot && draft.Assignee != "" && !issueHasAssignee(created, draft.Assignee) {
		if err := g.assignCopilot(ctx, created, draft); err != nil {
			warn = err
		} else {
			ref.Assignee = draft.Assignee
		}
	}
	if len(draft.Labels) > 0 && len(payload.Labels) == 0 {
		if err := g.Label(ctx, model.PullRequestRef{ID: strconv.Itoa(created.Number)}, draft.Labels); err != nil && warn == nil {
			warn = err
		}
	}
	if draft.Comment != "" {
		if err := g.postComment(ctx, created.Number, draft.Comment); err != nil && warn == nil {
			warn = err
		}
	}
	return ref, warn
}

func issueRef(g *GitHub, created ghIssue, draft IssueDraft) model.IssueRef {
	html := created.HTMLURL
	if html == "" {
		html = fmt.Sprintf("https://github.com/%s/%s/issues/%d", g.Owner, g.Repo, created.Number)
	}
	assignee := ""
	if len(created.Assignees) > 0 {
		assignee = created.Assignees[0].Login
	}
	if assignee == "" {
		assignee = draft.Assignee
	}
	if draft.Agent == config.FixAgentClaude {
		assignee = draft.Mention
	}
	if draft.Agent == config.FixAgentNone {
		assignee = ""
	}
	return model.IssueRef{
		Provider: "github",
		ID:       strconv.Itoa(created.Number),
		URL:      html,
		Title:    draft.Title,
		Assignee: assignee,
		Agent:    draft.Agent,
	}
}

func (g *GitHub) defaultBranch(ctx context.Context) string {
	var repo ghRepo
	if err := g.do(ctx, http.MethodGet, g.repo(), nil, &repo); err != nil || repo.DefaultBranch == "" {
		return "main"
	}
	return repo.DefaultBranch
}

func (g *GitHub) assignCopilot(ctx context.Context, created ghIssue, draft IssueDraft) error {
	base := draft.BaseBranch
	if base == "" {
		base = g.defaultBranch(ctx)
	}
	payload := map[string]any{
		"assignees": []string{draft.Assignee},
		"agent_assignment": ghAgentJob{
			TargetRepo:         g.Owner + "/" + g.Repo,
			BaseBranch:         base,
			CustomInstructions: draft.CustomInstructions,
			CustomAgent:        draft.CustomAgent,
		},
	}
	path := fmt.Sprintf("%s/issues/%d/assignees", g.repo(), created.Number)
	code, _, err := g.raw(ctx, http.MethodPost, path, payload, nil)
	if err != nil {
		return err
	}
	if code < 300 {
		return nil
	}
	if err := g.assignGraphQL(ctx, created, draft, base); err != nil {
		return fmt.Errorf("github copilot assign: status %d: %w", code, err)
	}
	return nil
}

func (g *GitHub) assignGraphQL(ctx context.Context, created ghIssue, draft IssueDraft, base string) error {
	const q = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    id
    issue(number: $number) { id }
    suggestedActors(capabilities: [CAN_BE_ASSIGNED], first: 100) {
      nodes { login ... on Bot { id } ... on User { id } }
    }
  }
}`
	var looked struct {
		Data struct {
			Repository struct {
				ID    string `json:"id"`
				Issue struct {
					ID string `json:"id"`
				} `json:"issue"`
				SuggestedActors struct {
					Nodes []struct {
						Login string `json:"login"`
						ID    string `json:"id"`
					} `json:"nodes"`
				} `json:"suggestedActors"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := g.graphql(ctx, q, map[string]any{
		"owner": g.Owner, "name": g.Repo, "number": created.Number,
	}, &looked); err != nil {
		return err
	}
	if len(looked.Errors) > 0 {
		return fmt.Errorf("graphql: %s", looked.Errors[0].Message)
	}
	actor := ""
	for _, n := range looked.Data.Repository.SuggestedActors.Nodes {
		if sameActor(n.Login, draft.Assignee) {
			actor = n.ID
			break
		}
	}
	if actor == "" {
		return fmt.Errorf("assignee %s is not a suggested actor", draft.Assignee)
	}
	issueID := created.NodeID
	if issueID == "" {
		issueID = looked.Data.Repository.Issue.ID
	}
	const mut = `mutation($id: ID!, $actors: [ID!]!, $repo: ID!, $base: String!, $instructions: String!, $agent: String!) {
  replaceActorsForAssignable(input: {
    assignableId: $id
    actorIds: $actors
    agentAssignment: { targetRepositoryId: $repo, baseRef: $base, customInstructions: $instructions, customAgent: $agent }
  }) { assignable { ... on Issue { number } } }
}`
	var done struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := g.graphql(ctx, mut, map[string]any{
		"id": issueID, "actors": []string{actor}, "repo": looked.Data.Repository.ID,
		"base": base, "instructions": draft.CustomInstructions, "agent": draft.CustomAgent,
	}, &done); err != nil {
		return err
	}
	if len(done.Errors) > 0 {
		return fmt.Errorf("graphql: %s", done.Errors[0].Message)
	}
	return nil
}

func (g *GitHub) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	code, body, err := g.raw(ctx, http.MethodPost, "/graphql", map[string]any{
		"query": query, "variables": vars,
	}, map[string]string{
		"GraphQL-Features": "coding_agent_model_selection,issues_copilot_assignment_api_support",
	})
	if err != nil {
		return err
	}
	if code >= 300 {
		return fmt.Errorf("github graphql status %d", code)
	}
	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return err
		}
	}
	return nil
}

func (g *GitHub) postComment(ctx context.Context, number int, body string) error {
	path := fmt.Sprintf("%s/issues/%d/comments", g.repo(), number)
	return g.do(ctx, http.MethodPost, path, map[string]string{"body": body}, nil)
}

// Label adds labels to the pull request. Pull request numbers are issue numbers.
func (g *GitHub) Label(ctx context.Context, ref model.PullRequestRef, labels []string) error {
	if ref.ID == "" || len(labels) == 0 {
		return nil
	}
	path := fmt.Sprintf("%s/issues/%s/labels", g.repo(), ref.ID)
	return g.do(ctx, http.MethodPost, path, map[string]any{"labels": labels}, nil)
}

// WaitCI polls check runs and the combined status. It does not merge.
func (g *GitHub) WaitCI(ctx context.Context, ref model.PullRequestRef, poll time.Duration) (string, error) {
	if ref.SHA == "" {
		return "none", fmt.Errorf("no commit sha")
	}
	if poll <= 0 {
		poll = 5 * time.Second
	}
	for {
		state, err := g.ciState(ctx, ref.SHA)
		if err != nil {
			return "", err
		}
		if state != "pending" {
			return state, nil
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "timeout", ctx.Err()
		case <-timer.C:
		}
	}
}

func (g *GitHub) ciState(ctx context.Context, sha string) (string, error) {
	var checks struct {
		TotalCount int `json:"total_count"`
		CheckRuns  []struct {
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	code, body, err := g.raw(ctx, http.MethodGet, g.repo()+"/commits/"+url.PathEscape(sha)+"/check-runs", nil, nil)
	if err != nil {
		return "", err
	}
	if code < 300 && len(body) > 0 {
		if err := json.Unmarshal(body, &checks); err != nil {
			return "", err
		}
	}
	if checks.TotalCount > 0 {
		pending := false
		for _, c := range checks.CheckRuns {
			if c.Status != "completed" {
				pending = true
				continue
			}
			switch c.Conclusion {
			case "success", "neutral", "skipped":
			default:
				return "failure", nil
			}
		}
		if pending {
			return "pending", nil
		}
		return "success", nil
	}
	var status struct {
		State      string `json:"state"`
		TotalCount int    `json:"total_count"`
	}
	code, body, err = g.raw(ctx, http.MethodGet, g.repo()+"/commits/"+url.PathEscape(sha)+"/status", nil, nil)
	if err != nil {
		return "", err
	}
	if code >= 300 {
		return "", fmt.Errorf("github status %d", code)
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &status); err != nil {
			return "", err
		}
	}
	if status.TotalCount == 0 || status.State == "" {
		return "none", nil
	}
	return status.State, nil
}

func issueHasAssignee(issue ghIssue, login string) bool {
	for _, a := range issue.Assignees {
		if sameActor(a.Login, login) {
			return true
		}
	}
	return false
}

func sameActor(a, b string) bool {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		return strings.TrimSuffix(s, "[bot]")
	}
	a, b = norm(a), norm(b)
	return a != "" && a == b
}
