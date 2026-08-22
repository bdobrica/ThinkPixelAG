package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.RunWorkerRepository = (*Repositories)(nil)

func (r *Repositories) ClaimRun(ctx context.Context, tenantID, workerID, leaseID domain.ID, now, expiresAt time.Time) (domain.RunLease, error) {
	if r == nil || r.db == nil || tenantID.IsZero() || workerID.IsZero() || leaseID.IsZero() || !expiresAt.After(now) {
		return domain.RunLease{}, errors.New("run claim is invalid")
	}
	var lease domain.RunLease
	err := r.withWorkerTransaction(ctx, func(db DBTX) error {
		var runID string
		var currentFence int64
		err := db.QueryRow(ctx, `SELECT id::text,fencing_token FROM runs
WHERE tenant_id=$1 AND state IN ('ADMITTED','RUNNING') AND (lease_expires_at IS NULL OR lease_expires_at <= $2)
ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1`, tenantID.String(), now).Scan(&runID, &currentFence)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrRunLeaseUnavailable
		}
		if err != nil {
			return fmt.Errorf("select claimable run: %w", err)
		}
		parsedRunID, err := domain.ParseID(runID)
		if err != nil {
			return fmt.Errorf("decode claimed run: %w", err)
		}
		fence, err := domain.NextFencingToken(currentFence)
		if err != nil {
			return domain.NewError(domain.CodeConflict, "run fencing token is exhausted")
		}
		result, err := db.Exec(ctx, `UPDATE runs SET lease_id=$3,lease_expires_at=$4,fencing_token=$5 WHERE tenant_id=$1 AND id=$2`, tenantID.String(), runID, leaseID.String(), expiresAt, fence)
		if err != nil || result.RowsAffected() != 1 {
			return fmt.Errorf("claim run: %w", err)
		}
		lease = domain.RunLease{RunID: parsedRunID, TenantID: tenantID, LeaseID: leaseID, WorkerID: workerID, FencingToken: fence, ExpiresAt: expiresAt}
		return appendWorkerEvent(ctx, db, lease, "run.claimed", nil, now, map[string]any{"lease_expires_at": expiresAt})
	})
	if err != nil {
		return domain.RunLease{}, workerRepositoryError(err)
	}
	return lease, nil
}

func (r *Repositories) HeartbeatRun(ctx context.Context, lease domain.RunLease, now, expiresAt time.Time) (domain.RunLease, error) {
	if r == nil || r.db == nil || lease.Validate() != nil || !expiresAt.After(now) || !expiresAt.After(lease.ExpiresAt) {
		return domain.RunLease{}, errors.New("run heartbeat is invalid")
	}
	err := r.withWorkerTransaction(ctx, func(db DBTX) error {
		var state string
		err := db.QueryRow(ctx, `SELECT state FROM runs WHERE tenant_id=$1 AND id=$2 AND lease_id=$3 AND fencing_token=$4 AND lease_expires_at>$5 FOR UPDATE`, lease.TenantID.String(), lease.RunID.String(), lease.LeaseID.String(), lease.FencingToken, now).Scan(&state)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrRunLeaseStale
		}
		if err != nil {
			return fmt.Errorf("lock heartbeat run: %w", err)
		}
		if domain.RunState(state).Terminal() {
			return domain.ErrRunLeaseStale
		}
		result, err := db.Exec(ctx, `UPDATE runs SET lease_expires_at=$5 WHERE tenant_id=$1 AND id=$2 AND lease_id=$3 AND fencing_token=$4`, lease.TenantID.String(), lease.RunID.String(), lease.LeaseID.String(), lease.FencingToken, expiresAt)
		if err != nil || result.RowsAffected() != 1 {
			return fmt.Errorf("heartbeat run: %w", err)
		}
		return appendWorkerEvent(ctx, db, lease, "run.heartbeat", nil, now, map[string]any{"lease_expires_at": expiresAt})
	})
	if err != nil {
		return domain.RunLease{}, workerRepositoryError(err)
	}
	lease.ExpiresAt = expiresAt
	return lease, nil
}

func (r *Repositories) MutateWorkerRun(ctx context.Context, lease domain.RunLease, operation domain.WorkerRunOperation, eventID domain.ID, now time.Time) (domain.Run, error) {
	if r == nil || r.db == nil || lease.Validate() != nil || eventID.IsZero() {
		return domain.Run{}, errors.New("worker run mutation is invalid")
	}
	target, actor, err := operation.Target()
	if err != nil {
		return domain.Run{}, err
	}
	err = r.withWorkerTransaction(ctx, func(db DBTX) error {
		var state string
		var version int64
		var updated time.Time
		var terminal *time.Time
		err := db.QueryRow(ctx, `SELECT state,state_version,updated_at,terminal_at FROM runs WHERE tenant_id=$1 AND id=$2 AND lease_id=$3 AND fencing_token=$4 AND lease_expires_at>$5 FOR UPDATE`, lease.TenantID.String(), lease.RunID.String(), lease.LeaseID.String(), lease.FencingToken, now).Scan(&state, &version, &updated, &terminal)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrRunLeaseStale
		}
		if err != nil {
			return fmt.Errorf("lock worker run: %w", err)
		}
		current := domain.RunLifecycle{State: domain.RunState(state), Version: version, UpdatedAt: updated.UTC(), TerminalAt: terminal}
		next, changed, err := current.Transition(domain.RunTransition{To: target, Actor: actor, ExpectedVersion: version, At: now})
		if err != nil {
			return domain.NewError(domain.CodeConflict, "worker operation conflicts with run state")
		}
		if !changed {
			return nil
		}
		clearLease := target.Terminal()
		result, err := db.Exec(ctx, `UPDATE runs SET state=$6,state_version=$7,updated_at=$5,terminal_at=$8,
lease_id=CASE WHEN $9 THEN NULL ELSE lease_id END,lease_expires_at=CASE WHEN $9 THEN NULL ELSE lease_expires_at END
WHERE tenant_id=$1 AND id=$2 AND lease_id=$3 AND fencing_token=$4`, lease.TenantID.String(), lease.RunID.String(), lease.LeaseID.String(), lease.FencingToken, now, string(target), next.Version, next.TerminalAt, clearLease)
		if err != nil || result.RowsAffected() != 1 {
			return fmt.Errorf("mutate worker run: %w", err)
		}
		return appendWorkerEventWithID(ctx, db, lease, eventID, "run."+workerEventSuffix(operation), actor, &next, now, map[string]any{"previous_state": state})
	})
	if err != nil {
		return domain.Run{}, workerRepositoryError(err)
	}
	tenantRepository, err := r.ForTenant(lease.TenantID)
	if err != nil {
		return domain.Run{}, err
	}
	record, err := tenantRepository.GetRun(ctx, lease.RunID)
	if err != nil {
		return domain.Run{}, err
	}
	return record.Run, nil
}

func appendWorkerEvent(ctx context.Context, db DBTX, lease domain.RunLease, eventType string, lifecycle *domain.RunLifecycle, at time.Time, payload map[string]any) error {
	id, err := domain.NewID()
	if err != nil {
		return err
	}
	return appendWorkerEventWithID(ctx, db, lease, id, eventType, domain.RunActorWorker, lifecycle, at, payload)
}

func appendWorkerEventWithID(ctx context.Context, db DBTX, lease domain.RunLease, eventID domain.ID, eventType string, actor domain.RunActor, lifecycle *domain.RunLifecycle, at time.Time, payload map[string]any) error {
	var sequence int64
	var state string
	var version int64
	if lifecycle == nil {
		if err := db.QueryRow(ctx, `SELECT state,state_version FROM runs WHERE tenant_id=$1 AND id=$2`, lease.TenantID.String(), lease.RunID.String()).Scan(&state, &version); err != nil {
			return err
		}
	} else {
		state, version = string(lifecycle.State), lifecycle.Version
	}
	if err := db.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM run_events WHERE tenant_id=$1 AND run_id=$2`, lease.TenantID.String(), lease.RunID.String()).Scan(&sequence); err != nil {
		return err
	}
	payload["fencing_token"] = lease.FencingToken
	encoded, _ := json.Marshal(payload)
	_, err := db.Exec(ctx, `INSERT INTO run_events(id,tenant_id,run_id,sequence,event_type,actor_type,actor_id,state,state_version,payload,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, eventID.String(), lease.TenantID.String(), lease.RunID.String(), sequence, eventType, string(actor), lease.WorkerID.String(), state, version, encoded, at)
	return err
}

func workerEventSuffix(operation domain.WorkerRunOperation) string {
	switch operation {
	case domain.WorkerRunStart:
		return "started"
	case domain.WorkerRunComplete:
		return "completed"
	case domain.WorkerRunFail:
		return "failed"
	default:
		return "timed_out"
	}
}

func workerRepositoryError(err error) error {
	if errors.Is(err, domain.ErrRunLeaseUnavailable) {
		return domain.NewError(domain.CodeNotFound, "no run is available")
	}
	if errors.Is(err, domain.ErrRunLeaseStale) {
		return domain.NewError(domain.CodeConflict, "run lease is stale")
	}
	return err
}

func (r *Repositories) withWorkerTransaction(ctx context.Context, fn func(DBTX) error) error {
	beginner, ok := r.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return fn(r.db)
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin worker transaction: %w", err)
	}
	if err = fn(tx); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit worker transaction: %w", err)
	}
	return nil
}
