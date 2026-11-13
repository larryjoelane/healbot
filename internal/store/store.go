// Package store groups signals into incidents. Shards keep the hot path off a
// single mutex. Returned incidents are clones.
package store

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/larryjoelane/healbot/internal/group"
	"github.com/larryjoelane/healbot/internal/model"
)

const maxSignalIDs = 32

// Store is an in-memory incident store.
type Store struct {
	shards  [group.ShardCount]shard
	index   sync.Map // incident id -> fingerprint
	history sync.Map // incident id -> model.Incident clone
	seq     atomic.Uint64
	now     func() time.Time
}

type shard struct {
	mu   sync.Mutex
	open map[uint64]*model.Incident
}

// AbsorbResult is what changed when a signal was grouped.
type AbsorbResult struct {
	Incident       model.Incident
	Created        bool
	SeverityRaised bool
}

// New returns an empty store.
func New(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	s := &Store{now: now}
	for i := range s.shards {
		s.shards[i].open = map[uint64]*model.Incident{}
	}
	return s
}

// Absorb groups sig into an open incident.
func (s *Store) Absorb(sig *model.Signal) AbsorbResult {
	fp := sig.Fingerprint
	if fp == 0 {
		fp = group.Fingerprint(sig.ResourceID, sig.ExceptionType, sig.TopFrame, sig.Message)
		sig.Fingerprint = fp
	}
	sh := &s.shards[fp%group.ShardCount]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	now := s.now()
	if sig.ReceivedAt.IsZero() {
		sig.ReceivedAt = now
	}
	if sig.OccurredAt.IsZero() {
		sig.OccurredAt = sig.ReceivedAt
	}
	inc := sh.open[fp]
	if inc == nil || inc.Status == model.StatusResolved || inc.Suppressed {
		created := s.newIncident(sig, fp, now)
		sh.open[fp] = created
		s.index.Store(created.ID, fp)
		cloned := created.Clone()
		s.history.Store(created.ID, cloned)
		return AbsorbResult{Incident: cloned, Created: true}
	}
	raised := sig.Severity < inc.Severity
	inc.Count++
	inc.LastSeen = sig.ReceivedAt
	if raised {
		inc.Severity = sig.Severity
	}
	if sig.ErrorRate > inc.ErrorRate {
		inc.ErrorRate = sig.ErrorRate
	}
	if sig.Message != "" {
		inc.Message = sig.Message
	}
	if sig.StackTrace != "" {
		inc.StackTrace = sig.StackTrace
	}
	if sig.ID != "" {
		inc.SignalIDs = append(inc.SignalIDs, sig.ID)
		if len(inc.SignalIDs) > maxSignalIDs {
			inc.SignalIDs = append([]string(nil), inc.SignalIDs[len(inc.SignalIDs)-maxSignalIDs:]...)
		}
	}
	cloned := inc.Clone()
	s.history.Store(inc.ID, cloned)
	return AbsorbResult{Incident: cloned, SeverityRaised: raised}
}

func (s *Store) newIncident(sig *model.Signal, fp uint64, now time.Time) *model.Incident {
	seen := sig.ReceivedAt
	if seen.IsZero() {
		seen = now
	}
	title := sig.ExceptionType
	if sig.TopFrame != "" && title != "" {
		title = title + " in " + sig.TopFrame
	}
	if title == "" {
		title = trimTitle(sig.Message)
	}
	if title == "" {
		title = "incident on " + sig.ResourceName
	}
	id := fmt.Sprintf("inc_%d", s.seq.Add(1))
	ids := []string{}
	if sig.ID != "" {
		ids = []string{sig.ID}
	}
	return &model.Incident{
		ID:            id,
		Fingerprint:   fmt.Sprintf("%016x", fp),
		Status:        model.StatusOpen,
		Severity:      sig.Severity,
		ResourceID:    sig.ResourceID,
		ResourceName:  sig.ResourceName,
		ResourceType:  sig.ResourceType,
		Title:         title,
		Operation:     sig.Operation,
		ExceptionType: sig.ExceptionType,
		Message:       sig.Message,
		StackTrace:    sig.StackTrace,
		TopFrame:      sig.TopFrame,
		ErrorRate:     sig.ErrorRate,
		FirstSeen:     seen,
		LastSeen:      seen,
		Count:         1,
		SignalIDs:     ids,
	}
}

// ResolveOpen marks the open incident for this signal resolved.
func (s *Store) ResolveOpen(sig *model.Signal) (model.Incident, bool) {
	fp := sig.Fingerprint
	if fp == 0 {
		fp = group.Fingerprint(sig.ResourceID, sig.ExceptionType, sig.TopFrame, sig.Message)
	}
	sh := &s.shards[fp%group.ShardCount]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	inc := sh.open[fp]
	if inc == nil || inc.Status == model.StatusResolved {
		return model.Incident{}, false
	}
	inc.Status = model.StatusResolved
	inc.LastSeen = s.now()
	cloned := inc.Clone()
	s.history.Store(inc.ID, cloned)
	delete(sh.open, fp)
	return cloned, true
}

// Update applies fn to the incident and returns a clone.
func (s *Store) Update(id string, fn func(*model.Incident)) (model.Incident, bool) {
	v, ok := s.index.Load(id)
	if !ok {
		return model.Incident{}, false
	}
	fp := v.(uint64)
	sh := &s.shards[fp%group.ShardCount]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	inc := sh.open[fp]
	if inc == nil || inc.ID != id {
		return model.Incident{}, false
	}
	fn(inc)
	cloned := inc.Clone()
	s.history.Store(inc.ID, cloned)
	if inc.Status == model.StatusResolved || inc.Suppressed {
		delete(sh.open, fp)
	}
	return cloned, true
}

// Get returns a clone by id, including incidents that already left the open set.
func (s *Store) Get(id string) (model.Incident, bool) {
	if v, ok := s.index.Load(id); ok {
		fp := v.(uint64)
		sh := &s.shards[fp%group.ShardCount]
		sh.mu.Lock()
		inc := sh.open[fp]
		if inc != nil && inc.ID == id {
			cloned := inc.Clone()
			sh.mu.Unlock()
			return cloned, true
		}
		sh.mu.Unlock()
	}
	if v, ok := s.history.Load(id); ok {
		return v.(model.Incident), true
	}
	return model.Incident{}, false
}

// List returns incidents, newest activity first. Open incidents win over a
// stale history copy of the same id.
func (s *Store) List() []model.Incident {
	seen := map[string]struct{}{}
	var all []model.Incident
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.Lock()
		for _, inc := range sh.open {
			all = append(all, inc.Clone())
			seen[inc.ID] = struct{}{}
		}
		sh.mu.Unlock()
	}
	s.history.Range(func(_, v any) bool {
		inc := v.(model.Incident)
		if _, ok := seen[inc.ID]; !ok {
			all = append(all, inc)
		}
		return true
	})
	sortIncidents(all)
	return all
}

func sortIncidents(all []model.Incident) {
	for i := 1; i < len(all); i++ {
		j := i
		for j > 0 && all[j].LastSeen.After(all[j-1].LastSeen) {
			all[j], all[j-1] = all[j-1], all[j]
			j--
		}
	}
}

func trimTitle(s string) string {
	if len(s) <= 80 {
		return s
	}
	return s[:80]
}
