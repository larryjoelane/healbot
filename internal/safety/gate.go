// Package safety enforces the kill switch, rate limit, cooldown, and circuit breaker.
package safety

import (
	"sync"
	"sync/atomic"
	"time"
)

// Limits are the numbers the gate enforces for one resource.
type Limits struct {
	Cooldown        time.Duration
	MaxPerHour      int
	BreakerFailures int
	BreakerOpenFor  time.Duration
}

// Permit is the gate's answer for one live mutation.
type Permit struct {
	Allowed bool
	Reason  string
}

// Gate is the process-wide safety interlock.
type Gate struct {
	killed atomic.Bool
	mu     sync.Mutex
	byRes  map[string]*state
}

type state struct {
	attempts  []time.Time
	last      time.Time
	failures  int
	openUntil time.Time
	halfOpen  bool
}

// New returns a gate. It starts closed (automation allowed) unless killed is set.
func New(killed bool) *Gate {
	g := &Gate{byRes: map[string]*state{}}
	if killed {
		g.killed.Store(true)
	}
	return g
}

// Killed reports the human override.
func (g *Gate) Killed() bool { return g.killed.Load() }

// SetKilled engages or releases the kill switch. The caller audits the change.
func (g *Gate) SetKilled(v bool) { g.killed.Store(v) }

// Admit decides whether a live tier-2 action may start.
func (g *Gate) Admit(resource string, now time.Time, lim Limits) Permit {
	if g.killed.Load() {
		return Permit{Reason: "killed"}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.state(resource)
	if !st.openUntil.IsZero() && now.Before(st.openUntil) {
		return Permit{Reason: "breaker_open"}
	}
	if !st.openUntil.IsZero() && !now.Before(st.openUntil) {
		st.openUntil = time.Time{}
		st.halfOpen = true
	}
	if lim.Cooldown > 0 && !st.last.IsZero() && now.Sub(st.last) < lim.Cooldown {
		return Permit{Reason: "cooldown"}
	}
	window := now.Add(-time.Hour)
	kept := st.attempts[:0]
	for _, ts := range st.attempts {
		if ts.After(window) {
			kept = append(kept, ts)
		}
	}
	st.attempts = kept
	max := lim.MaxPerHour
	if max < 1 {
		max = 1
	}
	if len(st.attempts) >= max {
		return Permit{Reason: "rate_limited"}
	}
	if st.halfOpen {
		return Permit{Allowed: true, Reason: "half_open"}
	}
	return Permit{Allowed: true, Reason: "ok"}
}

// Record stores the outcome of a live attempt. success resets the breaker.
func (g *Gate) Record(resource string, now time.Time, success bool, lim Limits) {
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.state(resource)
	st.attempts = append(st.attempts, now)
	st.last = now
	st.halfOpen = false
	if success {
		st.failures = 0
		st.openUntil = time.Time{}
		return
	}
	st.failures++
	need := lim.BreakerFailures
	if need < 1 {
		need = 1
	}
	if st.failures >= need {
		open := lim.BreakerOpenFor
		if open <= 0 {
			open = 30 * time.Minute
		}
		st.openUntil = now.Add(open)
		st.failures = 0
	}
}

func (g *Gate) state(resource string) *state {
	st := g.byRes[resource]
	if st == nil {
		st = &state{}
		g.byRes[resource] = st
	}
	return st
}
