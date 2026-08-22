package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.RunEventRepository = (*TenantRepository)(nil)

func (r *TenantRepository) ListRunEvents(ctx context.Context, runID domain.ID, after int64, limit int) (ports.RunEventWindow, error) {
	if err := r.valid(); err != nil {
		return ports.RunEventWindow{}, err
	}
	if runID.IsZero() || after < 0 || limit < 1 || limit > 256 {
		return ports.RunEventWindow{}, errors.New("invalid run event query")
	}
	var exists bool
	var oldest, latest int64
	err := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE tenant_id=$1 AND id=$2),COALESCE(min(sequence),0),COALESCE(max(sequence),0) FROM run_events WHERE tenant_id=$1 AND run_id=$2`, r.tenantID.String(), runID.String()).Scan(&exists, &oldest, &latest)
	if err != nil {
		return ports.RunEventWindow{}, fmt.Errorf("query run event window: %w", err)
	}
	if !exists {
		return ports.RunEventWindow{}, domain.NewError(domain.CodeNotFound, "run not found")
	}
	rows, err := r.db.Query(ctx, `SELECT id::text,run_id::text,sequence,event_type,payload,occurred_at FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND sequence>$3 ORDER BY sequence LIMIT $4`, r.tenantID.String(), runID.String(), after, limit)
	if err != nil {
		return ports.RunEventWindow{}, fmt.Errorf("query run events: %w", err)
	}
	defer rows.Close()
	window := ports.RunEventWindow{OldestRetained: oldest, Latest: latest}
	for rows.Next() {
		var event domain.RunEvent
		var id, rid string
		var payload []byte
		if err := rows.Scan(&id, &rid, &event.Sequence, &event.Type, &payload, &event.OccurredAt); err != nil {
			return ports.RunEventWindow{}, err
		}
		event.ID, err = domain.ParseID(id)
		if err != nil {
			return ports.RunEventWindow{}, err
		}
		event.RunID, err = domain.ParseID(rid)
		if err != nil {
			return ports.RunEventWindow{}, err
		}
		if err = json.Unmarshal(payload, &event.Data); err != nil {
			return ports.RunEventWindow{}, err
		}
		event.OccurredAt = event.OccurredAt.UTC()
		window.Events = append(window.Events, event)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ports.RunEventWindow{}, err
	}
	return window, nil
}
