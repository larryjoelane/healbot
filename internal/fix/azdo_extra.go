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
	"strings"
)

func (a *AzureDevOps) raw(ctx context.Context, method, path string, in any) (int, []byte, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base()+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "healbot")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.Token != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+a.Token)))
	}
	client := a.HTTP
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

// Read fetches one file from the default branch, or from ref when it is set.
func (a *AzureDevOps) Read(ctx context.Context, path, ref string) (string, bool, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	q := a.repo() + "/items?path=" + url.QueryEscape(path) + "&includeContent=true&api-version=7.1"
	if ref != "" {
		q += "&versionDescriptor.version=" + url.QueryEscape(ref) + "&versionDescriptor.versionType=branch"
	}
	code, body, err := a.raw(ctx, http.MethodGet, q, nil)
	if err != nil {
		return "", false, err
	}
	if code == http.StatusNotFound {
		return "", true, nil
	}
	if code >= 300 {
		return "", false, fmt.Errorf("azure devops item %s: status %d", path, code)
	}
	var obj struct {
		Content string `json:"content"`
		Path    string `json:"path"`
	}
	if json.Unmarshal(body, &obj) == nil && (obj.Path != "" || strings.Contains(string(body), `"content"`)) {
		return obj.Content, false, nil
	}
	return string(body), false, nil
}

// ListFiles returns file paths under dir, up to a small cap. A missing directory is empty.
func (a *AzureDevOps) ListFiles(ctx context.Context, dir, ref string) ([]string, error) {
	var out []string
	if err := a.walk(ctx, "/"+strings.Trim(dir, "/"), ref, 0, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *AzureDevOps) walk(ctx context.Context, dir, ref string, depth int, out *[]string) error {
	if depth > 4 || len(*out) >= 40 {
		return nil
	}
	q := a.repo() + "/items?scopePath=" + url.QueryEscape(dir) + "&recursionLevel=OneLevel&api-version=7.1"
	if ref != "" {
		q += "&versionDescriptor.version=" + url.QueryEscape(ref) + "&versionDescriptor.versionType=branch"
	}
	code, body, err := a.raw(ctx, http.MethodGet, q, nil)
	if err != nil {
		return err
	}
	if code == http.StatusNotFound {
		return nil
	}
	if code >= 300 {
		return fmt.Errorf("azure devops list %s: status %d", dir, code)
	}
	var page struct {
		Value []struct {
			Path          string `json:"path"`
			IsFolder      bool   `json:"isFolder"`
			GitObjectType string `json:"gitObjectType"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return err
	}
	for _, item := range page.Value {
		if len(*out) >= 40 {
			return nil
		}
		p := strings.TrimPrefix(item.Path, "/")
		if p == "" || "/"+p == dir || item.Path == dir {
			continue
		}
		folder := item.IsFolder || item.GitObjectType == "tree"
		if folder {
			if err := a.walk(ctx, item.Path, ref, depth+1, out); err != nil {
				return err
			}
			continue
		}
		*out = append(*out, p)
	}
	return nil
}
