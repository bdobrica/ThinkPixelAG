package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

// LoadOperationalMetrics reads bounded aggregate backlog state. It does not
// expose tenant identifiers or scan payload columns.
func (r *Repositories) LoadOperationalMetrics(ctx context.Context, observedAt time.Time) (ports.OperationalMetricsSnapshot, error) {
	var out ports.OperationalMetricsSnapshot
	if r == nil || r.db == nil || observedAt.IsZero() {
		return out, errors.New("operational metrics require repositories and observation time")
	}
	var outboxOldestSeconds, settlementLagSeconds float64
	err := r.db.QueryRow(ctx, `SELECT
COUNT(*) FILTER (WHERE published_at IS NULL AND dead_lettered_at IS NULL),
COALESCE(EXTRACT(EPOCH FROM ($1-MIN(occurred_at) FILTER (WHERE published_at IS NULL AND dead_lettered_at IS NULL))),0),
COALESCE((SELECT EXTRACT(EPOCH FROM ($1-MIN(r.terminal_at))) FROM runs r WHERE r.terminal_at IS NOT NULL AND EXISTS (SELECT 1 FROM resource_envelopes e JOIN resource_reservations rr ON rr.tenant_id=e.tenant_id AND rr.child_envelope_id=e.id AND rr.state='OPEN' WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id)),0)
FROM outbox_messages`, observedAt).Scan(&out.OutboxPending, &outboxOldestSeconds, &settlementLagSeconds)
	if err != nil {
		return out, fmt.Errorf("load operational metrics: %w", err)
	}
	out.OutboxOldestAge = nonnegativeSeconds(outboxOldestSeconds)
	out.OldestSettlementLag = nonnegativeSeconds(settlementLagSeconds)
	return out, nil
}

func nonnegativeSeconds(seconds float64) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}
