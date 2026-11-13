package pipeline

import (
	"context"
	"time"

	"github.com/larryjoelane/healbot/internal/model"
	"github.com/larryjoelane/healbot/internal/platform"
)

// Poller feeds Log Analytics query results into the pipeline on an interval.
type Poller struct {
	Every time.Duration
	KQL   string
	RT    platform.Runtime
	Pipe  *Pipeline
}

// Once runs the configured query and handles each row.
func (p *Poller) Once(ctx context.Context) (int, error) {
	if p.RT == nil || p.Pipe == nil || p.KQL == "" {
		return 0, nil
	}
	sigs, err := p.RT.QuerySignals(ctx, p.KQL)
	if err != nil {
		return 0, err
	}
	for i := range sigs {
		if sigs[i].Source == "" {
			sigs[i].Source = model.SourceLogAnalytics
		}
		if _, herr := p.Pipe.HandleSignal(ctx, &sigs[i]); herr != nil {
			return i, herr
		}
	}
	return len(sigs), nil
}

// Run calls Once immediately and then on Every until ctx is canceled.
func (p *Poller) Run(ctx context.Context) {
	every := p.Every
	if every <= 0 {
		every = time.Minute
	}
	_, _ = p.Once(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = p.Once(ctx)
		}
	}
}
