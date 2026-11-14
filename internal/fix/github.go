package fix

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/larryjoelane/healbot/internal/model"
)

// GitHub opens a pull request through the GitHub REST API.
// There is intentionally no merge method on this client.
type GitHub struct {
	BaseURL string
	Owner   string
	Repo    string
	Token   string
	HTTP    *http.Client
}

// Name implements Provider.
func (g *GitHub) Name() string { return "github" }

type ghRepo struct {
	DefaultBranch string `json:"default_branch"`
}
type ghRef struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}
type ghPR struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	Title   string `json:"title"`
}

// Create branches from the default branch, commits the proposed files, and opens a PR.
func (g *GitHub) Create(ctx context.Context, draft Draft) (model.PullRequestRef, error) {
	var repo ghRepo
	if err := g.do(ctx, http.MethodGet, g.repo()+"", nil, &repo); err != nil {
		return model.PullRequestRef{}, err
	}
	base := repo.DefaultBranch
	if base == "" {
		base = "main"
	}
	var ref ghRef
	if err := g.do(ctx, http.MethodGet, g.repo()+"/git/ref/heads/"+url.PathEscape(base), nil, &ref); err != nil {
		return model.PullRequestRef{}, err
	}
	branch := draft.Branch
	if err := g.do(ctx, http.MethodPost, g.repo()+"/git/refs", map[string]string{
		"ref": "refs/heads/" + branch,
		"sha": ref.Object.SHA,
	}, nil); err != nil {
		return model.PullRequestRef{}, err
	}
	var headSHA string
	for _, f := range draft.Files {
		path := strings.TrimPrefix(f.Path, "/")
		payload := map[string]string{
			"message": draft.Title,
			"content": base64.StdEncoding.EncodeToString([]byte(f.Content)),
			"branch":  branch,
		}
		if f.Update {
			sha, found, err := g.blobSHA(ctx, path, branch)
			if err != nil {
				return model.PullRequestRef{}, err
			}
			if found {
				payload["sha"] = sha
			}
		}
		var written struct {
			Commit struct {
				SHA string `json:"sha"`
			} `json:"commit"`
		}
		if err := g.do(ctx, http.MethodPut, g.repo()+"/contents/"+path, payload, &written); err != nil {
			return model.PullRequestRef{}, err
		}
		if written.Commit.SHA != "" {
			headSHA = written.Commit.SHA
		}
	}
	var pr ghPR
	if err := g.do(ctx, http.MethodPost, g.repo()+"/pulls", map[string]string{
		"title": draft.Title,
		"head":  branch,
		"base":  base,
		"body":  draft.Body,
	}, &pr); err != nil {
		return model.PullRequestRef{}, err
	}
	html := pr.HTMLURL
	if html == "" {
		html = fmt.Sprintf("https://github.com/%s/%s/pull/%d", g.Owner, g.Repo, pr.Number)
	}
	return model.PullRequestRef{
		Provider: "github",
		ID:       fmt.Sprintf("%d", pr.Number),
		URL:      html,
		Title:    draft.Title,
		Branch:   branch,
		SHA:      headSHA,
	}, nil
}

func (g *GitHub) repo() string {
	return "/repos/" + url.PathEscape(g.Owner) + "/" + url.PathEscape(g.Repo)
}

func (g *GitHub) do(ctx context.Context, method, path string, in any, out any) error {
	code, raw, err := g.raw(ctx, method, path, in, nil)
	if err != nil {
		return err
	}
	if code >= 300 {
		return fmt.Errorf("github %s %s: status %d", method, path, code)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("github decode: %w", err)
		}
	}
	return nil
}
