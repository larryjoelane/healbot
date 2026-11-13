// Package fake is the in-process Azure stand-in used by the simulator and tests.
package fake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/platform"
)

// Runtime records the mutations it would have performed.
type Runtime struct {
	mu          sync.Mutex
	Calls       []platform.Call
	Deployments map[string][]model.Deployment
	Status      model.HealthStatus
	HealthErr   error
	Signals     []model.Signal
	Fail        map[string]error
	Now         func() time.Time
}

// New returns a runtime whose default health is unhealthy, so a live action
// that nobody configured a probe for cannot look like a successful heal.
func New(now func() time.Time) *Runtime {
	if now == nil {
		now = time.Now
	}
	return &Runtime{
		Deployments: map[string][]model.Deployment{},
		Status:      model.HealthStatus{Healthy: false, Detail: "error rate still elevated"},
		Fail:        map[string]error{},
		Now:         now,
	}
}

// WithExampleDeploy seeds the demo API with a slot swap 12 minutes ago.
func (r *Runtime) WithExampleDeploy(resourceID string) *Runtime {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Deployments[resourceID] = []model.Deployment{{
		Name:   "v1.4.2",
		Slot:   "production",
		At:     r.Now().Add(-12 * time.Minute),
		Active: true,
	}, {
		Name:   "v1.4.1",
		Slot:   "staging",
		At:     r.Now().Add(-6 * time.Hour),
		Active: false,
	}}
	return r
}

func (r *Runtime) record(op, id, detail string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.Fail[op]; err != nil {
		r.Calls = append(r.Calls, platform.Call{Op: op, ResourceID: id, Detail: "error: " + err.Error()})
		return err
	}
	r.Calls = append(r.Calls, platform.Call{Op: op, ResourceID: id, Detail: detail})
	return nil
}

func (r *Runtime) Restart(_ context.Context, resourceID string) error {
	return r.record("restart", resourceID, "")
}

func (r *Runtime) SwapSlot(_ context.Context, resourceID, slot string) error {
	return r.record("slot_swap", resourceID, slot)
}

func (r *Runtime) ScaleOut(_ context.Context, resourceID string, delta, maxInstances int32) error {
	return r.record("scale_out", resourceID, fmt.Sprintf("+%d ceiling %d", delta, maxInstances))
}

func (r *Runtime) RestartRevision(_ context.Context, resourceID, revision string) error {
	return r.record("restart_revision", resourceID, revision)
}

func (r *Runtime) RollbackRevision(_ context.Context, resourceID, revision string) error {
	return r.record("rollback_revision", resourceID, revision)
}

func (r *Runtime) ListChanges(_ context.Context, resourceID string) ([]model.Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]model.Deployment(nil), r.Deployments[resourceID]...), nil
}

func (r *Runtime) Health(_ context.Context, _ string) (model.HealthStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Status, r.HealthErr
}

func (r *Runtime) QuerySignals(_ context.Context, _ string) ([]model.Signal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]model.Signal(nil), r.Signals...), nil
}

// Snapshot returns the mutations so far.
func (r *Runtime) Snapshot() []platform.Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]platform.Call(nil), r.Calls...)
}
