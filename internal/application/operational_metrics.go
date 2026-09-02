package application

import (
	"context"
	"errors"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

// OperationalMetricsPublisher refreshes non-authoritative backlog gauges.
// Failure leaves the previous sample in place and never changes readiness or
// governance behavior.
type OperationalMetricsPublisher struct {
	source ports.OperationalMetricsSource
	sink   ports.OperationalMetricsSink
	clock  domain.Clock
}

func NewOperationalMetricsPublisher(source ports.OperationalMetricsSource, sink ports.OperationalMetricsSink, clock domain.Clock) (*OperationalMetricsPublisher, error) {
	if source == nil || sink == nil || clock == nil {
		return nil, errors.New("operational metrics publisher dependencies are required")
	}
	return &OperationalMetricsPublisher{source: source, sink: sink, clock: clock}, nil
}

func (p *OperationalMetricsPublisher) Refresh(ctx context.Context) error {
	snapshot, err := p.source.LoadOperationalMetrics(ctx, p.clock.Now())
	if err != nil {
		return err
	}
	p.sink.SetOutbox(snapshot.OutboxPending, snapshot.OutboxOldestAge)
	p.sink.SetSettlementLag(snapshot.OldestSettlementLag)
	return nil
}

func (p *OperationalMetricsPublisher) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	_ = p.Refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = p.Refresh(ctx)
		}
	}
}
