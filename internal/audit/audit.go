// Package audit records every decision. High-volume signal events can be
// queued; safety decisions are written before the call returns.
package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
)

// Log is an append-only decision record with a memory ring for the API.
type Log struct {
	mu       sync.Mutex
	events   []model.AuditEvent
	max      int
	path     string
	f        *os.File
	w        *bufio.Writer
	syncMode bool
	ch       chan model.AuditEvent
	seq      atomic.Uint64
	dropped  atomic.Uint64
	done     chan struct{}
}

// New opens an optional JSONL file. syncMode writes before Record returns,
// which the demo and tests use so the transcript is complete.
func New(path string, max int, syncMode bool) (*Log, error) {
	if max < 1 {
		max = 1000
	}
	l := &Log{max: max, path: path, syncMode: syncMode, done: make(chan struct{})}
	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		l.f = f
		l.w = bufio.NewWriterSize(f, 64*1024)
	}
	if !syncMode {
		l.ch = make(chan model.AuditEvent, 8192)
		go l.loop()
	} else {
		close(l.done)
	}
	return l, nil
}

func (l *Log) loop() {
	defer close(l.done)
	for e := range l.ch {
		l.append(e)
	}
	if l.w != nil {
		_ = l.w.Flush()
	}
}

// Record stores an event. In async mode a full buffer drops the event and
// counts the gap. Decision methods should use RecordSync.
func (l *Log) Record(e model.AuditEvent) {
	l.prepare(&e)
	if l.syncMode {
		l.append(e)
		return
	}
	select {
	case l.ch <- e:
	default:
		n := l.dropped.Add(1)
		if n == 1 || n%1000 == 0 {
			l.append(model.AuditEvent{
				ID: l.nextID(), Time: time.Now().UTC(), Kind: model.AuditAuditSaturated,
				Actor: "system", Message: fmt.Sprintf("dropped %d high-volume audit events", n),
			})
		}
	}
}

// RecordSync writes before returning.
func (l *Log) RecordSync(e model.AuditEvent) {
	l.prepare(&e)
	l.append(e)
}

func (l *Log) prepare(e *model.AuditEvent) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	if e.Actor == "" {
		e.Actor = "system"
	}
	if e.ID == "" {
		e.ID = l.nextID()
	}
}

func (l *Log) nextID() string {
	return fmt.Sprintf("aud_%d", l.seq.Add(1))
}

func (l *Log) append(e model.AuditEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
	if len(l.events) > l.max {
		l.events = append([]model.AuditEvent(nil), l.events[len(l.events)-l.max:]...)
	}
	if l.w == nil {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = l.w.Write(b)
	_, _ = l.w.Write([]byte{'\n'})
	if l.syncMode {
		_ = l.w.Flush()
	}
}

// List returns the newest events up to limit, oldest first within that window.
func (l *Log) List(limit int) []model.AuditEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 || limit > len(l.events) {
		limit = len(l.events)
	}
	start := len(l.events) - limit
	out := make([]model.AuditEvent, limit)
	copy(out, l.events[start:])
	return out
}

// ByIncident filters the ring buffer.
func (l *Log) ByIncident(id string) []model.AuditEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []model.AuditEvent
	for _, e := range l.events {
		if e.IncidentID == id {
			out = append(out, e)
		}
	}
	return out
}

// Close flushes the writer.
func (l *Log) Close() error {
	if l.ch != nil {
		close(l.ch)
		<-l.done
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w != nil {
		_ = l.w.Flush()
	}
	if l.f != nil {
		return l.f.Close()
	}
	return nil
}
