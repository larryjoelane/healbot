package fix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/larryjoelane/healbot/internal/model"
)

// AzureDevOps opens a pull request in Azure DevOps Repos.
// There is intentionally no merge or complete method on this client.
type AzureDevOps struct {
	BaseURL string
	Org     string
	Project string
	Repo    string
	Token   string
	HTTP    *http.Client
}

// Name implements Provider.
func (a *AzureDevOps) Name() string { return "azure_devops" }

// Create pushes a commit on a new branch and opens a pull request.
func (a *AzureDevOps) Create(ctx context.Context, draft Draft) (model.PullRequestRef, error) {
	var repo struct {
		ID            string `json:"id"`
		DefaultBranch string `json:"defaultBranch"`
	}
	if err := a.do(ctx, http.MethodGet, a.repo()+"?api-version=7.1", nil, &repo); err != nil {
		return model.PullRequestRef{}, err
	}
	base := repo.DefaultBranch
	if base == "" {
		base = "refs/heads/main"
	}
	if !strings.HasPrefix(base, "refs/") {
		base = "refs/heads/" + base
	}
	var refs struct {
		Value []struct {
			Name     string `json:"name"`
			ObjectID string `json:"objectId"`
		} `json:"value"`
	}
	filter := url.QueryEscape("heads/" + strings.TrimPrefix(base, "refs/heads/"))
	if err := a.do(ctx, http.MethodGet, a.repo()+"/refs?filter="+filter+"&api-version=7.1", nil, &refs); err != nil {
		return model.PullRequestRef{}, err
	}
	if len(refs.Value) == 0 || refs.Value[0].ObjectID == "" {
		return model.PullRequestRef{}, fmt.Errorf("azure devops: base branch %s has no object id", base)
	}
	changes := make([]map[string]any, 0, len(draft.Files))
	for _, f := range draft.Files {
		path := "/" + strings.TrimPrefix(f.Path, "/")
		change := "add"
		if f.Update {
			change = "edit"
		}
		changes = append(changes, map[string]any{
			"changeType": change,
			"item":       map[string]string{"path": path},
			"newContent": map[string]string{"content": f.Content, "contentType": "rawtext"},
		})
	}
	branch := "refs/heads/" + draft.Branch
	push := map[string]any{
		"refUpdates": []map[string]string{{"name": branch, "oldObjectId": refs.Value[0].ObjectID}},
		"commits": []map[string]any{{
			"comment": draft.Title,
			"changes": changes,
		}},
	}
	if err := a.do(ctx, http.MethodPost, a.repo()+"/pushes?api-version=7.1", push, nil); err != nil {
		return model.PullRequestRef{}, err
	}
	var pr struct {
		ID  int    `json:"pullRequestId"`
		URL string `json:"url"`
	}
	if err := a.do(ctx, http.MethodPost, a.repo()+"/pullrequests?api-version=7.1", map[string]string{
		"title":         draft.Title,
		"description":   draft.Body,
		"sourceRefName": branch,
		"targetRefName": base,
	}, &pr); err != nil {
		return model.PullRequestRef{}, err
	}
	web := pr.URL
	if web == "" {
		web = fmt.Sprintf("%s/%s/%s/_git/%s/pullrequest/%d", strings.TrimRight(a.base(), "/"), a.Org, a.Project, a.Repo, pr.ID)
	}
	return model.PullRequestRef{
		Provider: "azure_devops",
		ID:       fmt.Sprintf("%d", pr.ID),
		URL:      web,
		Title:    draft.Title,
		Branch:   draft.Branch,
	}, nil
}

func (a *AzureDevOps) base() string {
	if a.BaseURL != "" {
		return strings.TrimRight(a.BaseURL, "/")
	}
	return "https://dev.azure.com"
}

func (a *AzureDevOps) repo() string {
	return "/" + url.PathEscape(a.Org) + "/" + url.PathEscape(a.Project) + "/_apis/git/repositories/" + url.PathEscape(a.Repo)
}

func (a *AzureDevOps) do(ctx context.Context, method, path string, in any, out any) error {
	code, raw, err := a.raw(ctx, method, path, in)
	if err != nil {
		return err
	}
	if code >= 300 {
		return fmt.Errorf("azure devops %s %s: status %d", method, path, code)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("azure devops decode: %w", err)
		}
	}
	return nil
}
