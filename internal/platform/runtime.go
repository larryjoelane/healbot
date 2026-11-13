// Package platform is the boundary between Healbot and Azure. Tests and the
// simulator supply a fake. The azure package supplies the SDK implementation.
package platform

import (
	"context"
	"errors"

	"github.com/larryjoelane/healbot/internal/model"
)

// ErrAtCapacity means scale-out would pass the configured ceiling.
var ErrAtCapacity = errors.New("already at the configured instance ceiling")

// Runtime is the least-privilege surface Healbot needs from Azure.
// Implementations must not expose arbitrary ARM writes.
type Runtime interface {
	Restart(ctx context.Context, resourceID string) error
	SwapSlot(ctx context.Context, resourceID, slot string) error
	ScaleOut(ctx context.Context, resourceID string, delta, maxInstances int32) error
	RestartRevision(ctx context.Context, resourceID, revision string) error
	RollbackRevision(ctx context.Context, resourceID, revision string) error
	ListChanges(ctx context.Context, resourceID string) ([]model.Deployment, error)
	Health(ctx context.Context, resourceID string) (model.HealthStatus, error)
	QuerySignals(ctx context.Context, kql string) ([]model.Signal, error)
}

// Call records a mutation for tests.
type Call struct {
	Op         string
	ResourceID string
	Detail     string
}
