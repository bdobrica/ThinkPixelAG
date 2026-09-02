package ports

import (
	"context"
	"time"
)

// OperationalMetricsSnapshot contains bounded, deployment-wide backlog
// signals. It deliberately carries no tenant or resource identifiers.
type OperationalMetricsSnapshot struct {
	OutboxPending       int
	OutboxOldestAge     time.Duration
	OldestSettlementLag time.Duration
}

type OperationalMetricsSource interface {
	LoadOperationalMetrics(context.Context, time.Time) (OperationalMetricsSnapshot, error)
}

type OperationalMetricsSink interface {
	SetOutbox(int, time.Duration)
	SetSettlementLag(time.Duration)
}
