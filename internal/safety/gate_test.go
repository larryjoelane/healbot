package safety

import (
	"testing"
	"time"
)

func TestKillSwitch(t *testing.T) {
	g := New(false)
	if g.Killed() {
		t.Fatal("default is not killed")
	}
	g.SetKilled(true)
	p := g.Admit("res", time.Now(), Limits{MaxPerHour: 3})
	if p.Allowed || p.Reason != "killed" {
		t.Fatalf("%+v", p)
	}
	g.SetKilled(false)
	p = g.Admit("res", time.Now(), Limits{MaxPerHour: 3})
	if !p.Allowed {
		t.Fatalf("%+v", p)
	}
}

func TestRateAndCooldown(t *testing.T) {
	g := New(false)
	start := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	lim := Limits{Cooldown: time.Minute, MaxPerHour: 2, BreakerFailures: 3, BreakerOpenFor: time.Hour}
	if p := g.Admit("api", start, lim); !p.Allowed {
		t.Fatal(p)
	}
	g.Record("api", start, true, lim)
	if p := g.Admit("api", start.Add(10*time.Second), lim); p.Reason != "cooldown" {
		t.Fatal(p)
	}
	if p := g.Admit("api", start.Add(2*time.Minute), lim); !p.Allowed {
		t.Fatal(p)
	}
	g.Record("api", start.Add(2*time.Minute), true, lim)
	if p := g.Admit("api", start.Add(4*time.Minute), lim); p.Reason != "rate_limited" {
		t.Fatal(p)
	}
}

func TestBreaker(t *testing.T) {
	g := New(false)
	start := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	lim := Limits{MaxPerHour: 10, BreakerFailures: 2, BreakerOpenFor: 30 * time.Minute}
	g.Record("api", start, false, lim)
	if p := g.Admit("api", start.Add(time.Second), lim); !p.Allowed {
		t.Fatal(p)
	}
	g.Record("api", start.Add(time.Second), false, lim)
	if p := g.Admit("api", start.Add(2*time.Second), lim); p.Reason != "breaker_open" {
		t.Fatal(p)
	}
	if p := g.Admit("api", start.Add(31*time.Minute), lim); !p.Allowed || p.Reason != "half_open" {
		t.Fatal(p)
	}
	g.Record("api", start.Add(31*time.Minute), true, lim)
	if p := g.Admit("api", start.Add(32*time.Minute), lim); !p.Allowed || p.Reason != "ok" {
		t.Fatal(p)
	}
}
