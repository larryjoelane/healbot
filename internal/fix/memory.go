package fix

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
)

// Pull is a stored mock pull request.
type Pull struct {
	Ref    model.PullRequestRef
	Body   string
	Files  []File
	Labels []string
}

// IssueRecord is a stored mock issue.
type IssueRecord struct {
	Ref     model.IssueRef
	Body    string
	Comment string
	Labels  []string
}

// Memory is the demo repository. It records pull requests and issues and never merges them.
type Memory struct {
	mu       sync.Mutex
	pulls    map[string]Pull
	issues   map[string]IssueRecord
	files    map[string]string
	seq      int
	issueSeq int
	Base     string
	// CIState is returned by WaitCI. Empty means success.
	CIState string
}

// NewMemory returns an empty provider.
func NewMemory() *Memory {
	return &Memory{
		pulls:  map[string]Pull{},
		issues: map[string]IssueRecord{},
		files:  map[string]string{},
		Base:   "/pulls/",
	}
}

// Name implements Provider.
func (m *Memory) Name() string { return "mock" }

// AddFile seeds a file the source reader can return.
func (m *Memory) AddFile(path, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		m.files = map[string]string{}
	}
	m.files[strings.TrimPrefix(path, "/")] = content
}

// Create stores the draft.
func (m *Memory) Create(_ context.Context, draft Draft) (model.PullRequestRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id := fmt.Sprintf("pr_%d", m.seq)
	ref := model.PullRequestRef{
		Provider: "mock",
		ID:       id,
		URL:      m.Base + id,
		Title:    draft.Title,
		Branch:   draft.Branch,
		SHA:      fmt.Sprintf("sha_%d", m.seq),
	}
	m.pulls[id] = Pull{Ref: ref, Body: draft.Body, Files: append([]File(nil), draft.Files...)}
	return ref, nil
}

// Get returns one pull request.
func (m *Memory) Get(id string) (Pull, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pulls[id]
	return p, ok
}

// List returns pull requests in creation order.
func (m *Memory) List() []Pull {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Pull, 0, len(m.pulls))
	for i := 1; i <= m.seq; i++ {
		if p, ok := m.pulls[fmt.Sprintf("pr_%d", i)]; ok {
			out = append(out, p)
		}
	}
	return out
}

// Read implements Source.
func (m *Memory) Read(_ context.Context, path, _ string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files == nil {
		return "", true, nil
	}
	content, ok := m.files[strings.TrimPrefix(path, "/")]
	if !ok {
		return "", true, nil
	}
	return content, false, nil
}

// ListFiles implements Source.
func (m *Memory) ListFiles(_ context.Context, dir, _ string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir = strings.Trim(dir, "/")
	var out []string
	for p := range m.files {
		if dir == "" || p == dir || strings.HasPrefix(p, dir+"/") {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	if len(out) > 40 {
		out = out[:40]
	}
	return out, nil
}

// CreateIssue stores an issue and does not open or merge a pull request.
func (m *Memory) CreateIssue(_ context.Context, draft IssueDraft) (model.IssueRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.issues == nil {
		m.issues = map[string]IssueRecord{}
	}
	m.issueSeq++
	id := fmt.Sprintf("issue_%d", m.issueSeq)
	assignee := draft.Assignee
	if draft.Agent == "claude" && assignee == "" {
		assignee = draft.Mention
	}
	ref := model.IssueRef{
		Provider: "mock",
		ID:       id,
		URL:      "/issues/" + id,
		Title:    draft.Title,
		Assignee: assignee,
		Agent:    draft.Agent,
	}
	m.issues[id] = IssueRecord{
		Ref: ref, Body: draft.Body, Comment: draft.Comment, Labels: append([]string(nil), draft.Labels...),
	}
	return ref, nil
}

// GetIssue returns one stored issue.
func (m *Memory) GetIssue(id string) (IssueRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.issues[id]
	return rec, ok
}

// WaitCI returns CIState, or success when it was not set.
func (m *Memory) WaitCI(_ context.Context, _ model.PullRequestRef, _ time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CIState == "" {
		return "success", nil
	}
	return m.CIState, nil
}

// Label records labels on a mock pull request.
func (m *Memory) Label(_ context.Context, ref model.PullRequestRef, labels []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pulls[ref.ID]
	if !ok {
		return fmt.Errorf("mock pull %s not found", ref.ID)
	}
	p.Labels = append(p.Labels, labels...)
	p.Ref.CI = ref.CI
	m.pulls[ref.ID] = p
	return nil
}
