package fix

import (
	"context"
	"fmt"
	"sync"

	"github.com/larryjoelane/healbot/internal/model"
)

// Pull is a stored mock pull request.
type Pull struct {
	Ref   model.PullRequestRef
	Body  string
	Files []File
}

// Memory is the demo repository. It records pull requests and never merges them.
type Memory struct {
	mu    sync.Mutex
	pulls map[string]Pull
	seq   int
	Base  string
}

// NewMemory returns an empty provider.
func NewMemory() *Memory {
	return &Memory{pulls: map[string]Pull{}, Base: "/pulls/"}
}

// Name implements Provider.
func (m *Memory) Name() string { return "mock" }

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
