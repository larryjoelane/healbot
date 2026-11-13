// Package ingest turns Azure Monitor webhooks and log streams into signals.
// Log lines are parsed with one buffer allocation per line. A bounded worker
// pool applies backpressure when diagnosis cannot keep up.
package ingest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/larryjoelane/healbot/internal/azureid"
	"github.com/larryjoelane/healbot/internal/model"
)

// ErrSaturated is returned when a bounded queue is full.
var ErrSaturated = errors.New("ingest queue saturated")

// ErrClosed is returned when a pool has been shut down.
var ErrClosed = errors.New("ingest pool closed")

var errEmpty = errors.New("empty log line")
var errShape = errors.New("log line: want timestamp|severity|resource|exception|operation|frame|message")

// ParseLogLine parses one line into dst. String fields are copied into dst.Buf
// so the caller may reuse or overwrite line. dst.Buf's spare capacity is kept.
func ParseLogLine(line []byte, dst *model.Signal) error {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return errEmpty
	}
	if line[0] == '{' {
		return parseLogJSON(line, dst)
	}
	return parsePipe(line, dst)
}

func parsePipe(line []byte, dst *model.Signal) error {
	var cuts [6]int
	n := 0
	for i, c := range line {
		if c == '|' {
			cuts[n] = i
			n++
			if n == 6 {
				break
			}
		}
	}
	if n < 6 {
		return errShape
	}
	fields := [7][]byte{
		line[:cuts[0]],
		line[cuts[0]+1 : cuts[1]],
		line[cuts[1]+1 : cuts[2]],
		line[cuts[2]+1 : cuts[3]],
		line[cuts[3]+1 : cuts[4]],
		line[cuts[4]+1 : cuts[5]],
		line[cuts[5]+1:],
	}
	buf := dst.Buf[:0]
	need := 0
	for _, f := range fields {
		need += len(f)
	}
	if cap(buf) < need {
		buf = make([]byte, 0, need)
	}
	buf = buf[:need]
	off := 0
	for _, f := range fields {
		copy(buf[off:], f)
		off += len(f)
	}
	dst.Buf = buf
	off = 0
	take := func(n int) string {
		s := unsafeString(buf[off : off+n])
		off += n
		return s
	}
	ts := take(len(fields[0]))
	sevRaw := take(len(fields[1]))
	resource := take(len(fields[2]))
	exception := take(len(fields[3]))
	operation := take(len(fields[4]))
	frame := take(len(fields[5]))
	message := take(len(fields[6]))

	dst.Source = model.SourceLogBatch
	dst.ResourceID = resource
	dst.ResourceName = lastSegment(resource)
	dst.ResourceType = classify(resource)
	dst.ExceptionType = exception
	dst.Operation = operation
	dst.TopFrame = frame
	dst.Message = message
	dst.StackTrace = ""
	dst.Properties = nil
	dst.Resolved = false
	dst.ErrorRate = 0
	dst.Fingerprint = 0
	dst.ID = ""
	if t, ok := parseTimeUTC(ts); ok {
		dst.OccurredAt = t
	} else if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		dst.OccurredAt = t
	}
	if sev, ok := model.ParseSeverity(sevRaw); ok {
		dst.Severity = sev
	} else {
		dst.Severity = model.Sev3
	}
	return nil
}

func parseLogJSON(line []byte, dst *model.Signal) error {
	var raw map[string]any
	if err := json.Unmarshal(line, &raw); err != nil {
		return fmt.Errorf("log json: %w", err)
	}
	*dst = model.Signal{Buf: dst.Buf[:0], Source: model.SourceLogBatch, Severity: model.Sev3}
	dst.ResourceID = firstString(raw, "resourceId", "resource_id", "ResourceId")
	dst.ResourceName = lastSegment(dst.ResourceID)
	dst.ResourceType = classify(dst.ResourceID)
	dst.ExceptionType = firstString(raw, "exceptionType", "problemId", "type")
	dst.Message = firstString(raw, "message", "outerMessage", "innermostMessage")
	dst.Operation = firstString(raw, "operation", "operation_Name", "operationName")
	dst.TopFrame = firstString(raw, "topFrame", "frame")
	dst.StackTrace = firstString(raw, "stack", "stackTrace", "details")
	if dst.TopFrame == "" {
		dst.TopFrame = TopFrame(dst.StackTrace)
	}
	if sev, ok := model.ParseSeverity(firstString(raw, "severity", "sev")); ok {
		dst.Severity = sev
	}
	if ts := firstString(raw, "timestamp", "time", "occurred_at"); ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			dst.OccurredAt = t
		}
	}
	if v, ok := raw["errorRate"]; ok {
		dst.ErrorRate = asFloat(v)
	}
	return nil
}

func firstString(raw map[string]any, keys ...string) string {
	lower := map[string]any{}
	for k, v := range raw {
		lower[strings.ToLower(k)] = v
	}
	for _, k := range keys {
		if v, ok := lower[strings.ToLower(k)]; ok {
			return strings.TrimSpace(asString(v))
		}
	}
	return ""
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	case json.Number:
		f, _ := t.Float64()
		return f
	default:
		return 0
	}
}

// ParseLogStream scans NDJSON or pipe-delimited lines and calls fn for each.
// fn owns the signal only for the duration of the call; the next line reuses
// the struct but not the strings, which were copied.
func ParseLogStream(r io.Reader, fn func(*model.Signal) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	var sig model.Signal
	for sc.Scan() {
		if err := ParseLogLine(sc.Bytes(), &sig); err != nil {
			if errors.Is(err, errEmpty) {
				continue
			}
			return err
		}
		if err := fn(&sig); err != nil {
			return err
		}
	}
	return sc.Err()
}

// Detach copies every string off the parser buffer so the signal can outlive
// the next ParseLogLine call on the same destination.
func Detach(sig *model.Signal) model.Signal {
	if sig == nil {
		return model.Signal{}
	}
	out := *sig
	out.ID = strings.Clone(sig.ID)
	out.ResourceID = strings.Clone(sig.ResourceID)
	out.ResourceName = strings.Clone(sig.ResourceName)
	out.ResourceType = strings.Clone(sig.ResourceType)
	out.Operation = strings.Clone(sig.Operation)
	out.Message = strings.Clone(sig.Message)
	out.ExceptionType = strings.Clone(sig.ExceptionType)
	out.StackTrace = strings.Clone(sig.StackTrace)
	out.TopFrame = strings.Clone(sig.TopFrame)
	out.Buf = nil
	if sig.Properties != nil {
		out.Properties = make(map[string]string, len(sig.Properties))
		for k, v := range sig.Properties {
			out.Properties[k] = v
		}
	}
	return out
}
func TopFrame(stack string) string {
	if stack == "" {
		return ""
	}
	for _, line := range strings.Split(stack, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "at ")
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		if i := strings.IndexByte(line, '('); i > 0 {
			line = strings.TrimSpace(line[:i])
		}
		if i := strings.LastIndex(line, "."); i > 0 && i < len(line)-1 {
			return line[strings.LastIndex(line[:i], ".")+1:]
		}
		return line
	}
	return ""
}

func lastSegment(id string) string {
	if id == "" {
		return ""
	}
	if i := strings.LastIndex(id, "/"); i >= 0 && i < len(id)-1 {
		return id[i+1:]
	}
	return id
}

func classify(id string) string {
	switch {
	case strings.Contains(id, "Microsoft.Web/sites"):
		return model.TypeWebSite
	case strings.Contains(id, "Microsoft.Web/serverfarms"):
		return model.TypeServerFarm
	case strings.Contains(id, "Microsoft.App/containerApps"):
		return model.TypeContainerApp
	default:
		if ref, err := azureid.Parse(id); err == nil {
			return ref.ResourceType()
		}
		return ""
	}
}

func parseTimeUTC(s string) (time.Time, bool) {
	// 2026-11-13T14:02:01Z or 2026-11-13T14:02:01.123Z
	if len(s) < 20 || s[4] != '-' || s[7] != '-' || s[10] != 'T' || s[13] != ':' || s[16] != ':' {
		return time.Time{}, false
	}
	y, ok1 := atoi(s[0:4])
	mo, ok2 := atoi(s[5:7])
	d, ok3 := atoi(s[8:10])
	h, ok4 := atoi(s[11:13])
	mi, ok5 := atoi(s[14:16])
	sec, ok6 := atoi(s[17:19])
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
		return time.Time{}, false
	}
	ns := 0
	rest := s[19:]
	if len(rest) > 0 && rest[0] == '.' {
		frac := rest[1:]
		if n := strings.IndexByte(frac, 'Z'); n >= 0 {
			frac = frac[:n]
		} else if n := strings.IndexAny(frac, "+-"); n >= 0 {
			return time.Time{}, false
		}
		if len(frac) > 9 {
			frac = frac[:9]
		}
		for _, c := range frac {
			if c < '0' || c > '9' {
				return time.Time{}, false
			}
		}
		scale := 9 - len(frac)
		n, ok := atoi(frac)
		if !ok {
			return time.Time{}, false
		}
		for i := 0; i < scale; i++ {
			n *= 10
		}
		ns = n
		rest = "Z"
	}
	if rest != "Z" && rest != "z" {
		return time.Time{}, false
	}
	return time.Date(y, time.Month(mo), d, h, mi, sec, ns, time.UTC), true
}

func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func unsafeString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// Pool is a fixed set of workers reading from a bounded queue.
type Pool struct {
	jobs   chan func()
	wg     sync.WaitGroup
	closed bool
	mu     sync.Mutex
}

// NewPool starts workers. queue is the number of waiting jobs, not including
// the one a worker is already running.
func NewPool(workers, queue int) *Pool {
	if workers < 1 {
		workers = 1
	}
	if queue < 1 {
		queue = 1
	}
	p := &Pool{jobs: make(chan func(), queue)}
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			for fn := range p.jobs {
				fn()
			}
		}()
	}
	return p
}

// Submit blocks until the job is queued or ctx ends. This is the backpressure
// path used by the log stream: a slow diagnoser stops the scanner from reading.
func (p *Pool) Submit(ctx context.Context, fn func()) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrClosed
	}
	ch := p.jobs
	p.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ch <- fn:
		return nil
	}
}

// TrySubmit fails immediately when the queue is full.
func (p *Pool) TrySubmit(fn func()) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrClosed
	}
	ch := p.jobs
	p.mu.Unlock()
	select {
	case ch <- fn:
		return nil
	default:
		return ErrSaturated
	}
}

// Close waits for queued jobs to finish. Producers must have stopped.
func (p *Pool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.jobs)
	p.mu.Unlock()
	p.wg.Wait()
}
