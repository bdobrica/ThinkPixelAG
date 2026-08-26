package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.ResourceReconciliationRepository = (*Repositories)(nil)

// ReconcileNextReservation closes one terminal orphan or one expired,
// abandoned child. Selection and reclaim share a transaction; SKIP LOCKED lets
// replicas make progress independently, while the reservation lock and unique
// settlement constraint make crash/concurrent replay exactly once.
func (r *Repositories) ReconcileNextReservation(ctx context.Context, tenantID, systemPrincipalID domain.ID, now time.Time, evidence ports.ResourceReconciliationEvidence) (result domain.ResourceReconciliationResult, err error) {
	if r == nil || r.db == nil || tenantID.IsZero() || systemPrincipalID.IsZero() || evidence.SettlementID.IsZero() || evidence.PolicyDecisionID.IsZero() || evidence.AuditID.IsZero() || evidence.OutboxID.IsZero() || evidence.RunEventID.IsZero() {
		return result, errors.New("resource reconciliation request is invalid")
	}
	now, err = domain.RequireUTC(now)
	if err != nil || now.IsZero() {
		return result, errors.New("resource reconciliation time is invalid")
	}
	err = r.withWorkerTransaction(ctx, func(db DBTX) error {
		var reservationID, parentEnvelope, childEnvelope, childRun, runState string
		var runVersion, fencingToken int64
		var runUpdated time.Time
		var expiresAt *time.Time
		selectErr := db.QueryRow(ctx, `SELECT rr.id::text,rr.parent_envelope_id::text,rr.child_envelope_id::text,rr.child_run_id::text,rr.expires_at,ru.state,ru.state_version,ru.updated_at,ru.fencing_token
FROM resource_reservations rr
JOIN runs ru ON ru.tenant_id=rr.tenant_id AND ru.id=rr.child_run_id
JOIN principals p ON p.tenant_id=rr.tenant_id AND p.id=$2 AND p.principal_type='SYSTEM' AND p.disabled_at IS NULL
WHERE rr.tenant_id=$1 AND rr.state='OPEN'
  AND (ru.state IN ('COMPLETED','FAILED','CANCELLED','TIMED_OUT','FAILED_BUDGET')
       OR (rr.expires_at IS NOT NULL AND rr.expires_at <= $3 AND ru.state IN ('ADMITTED','RUNNING','PAUSED_FOR_BUDGET')))
  AND NOT EXISTS (SELECT 1 FROM resource_balances child_balance WHERE child_balance.tenant_id=rr.tenant_id AND child_balance.envelope_id=rr.child_envelope_id AND child_balance.allocated_open_value<>0)
ORDER BY CASE WHEN ru.state IN ('COMPLETED','FAILED','CANCELLED','TIMED_OUT','FAILED_BUDGET') THEN 0 ELSE 1 END,COALESCE(rr.expires_at,rr.created_at),rr.id
FOR UPDATE OF rr,ru SKIP LOCKED LIMIT 1`, tenantID.String(), systemPrincipalID.String(), now).Scan(&reservationID, &parentEnvelope, &childEnvelope, &childRun, &expiresAt, &runState, &runVersion, &runUpdated, &fencingToken)
		if errors.Is(selectErr, pgx.ErrNoRows) {
			return domain.ErrResourceReconciliationUnavailable
		}
		if selectErr != nil {
			return fmt.Errorf("claim resource reconciliation: %w", selectErr)
		}

		expired := !domain.RunState(runState).Terminal()
		if expired {
			if fencingToken == math.MaxInt64 {
				return domain.NewError(domain.CodeConflict, "expired run fencing token is exhausted")
			}
			current := domain.RunLifecycle{State: domain.RunState(runState), Version: runVersion, UpdatedAt: runUpdated.UTC()}
			next, changed, transitionErr := current.Transition(domain.RunTransition{To: domain.RunTimedOut, Actor: domain.RunActorSystem, ExpectedVersion: runVersion, At: now})
			if transitionErr != nil || !changed {
				return domain.WrapError(domain.CodeConflict, "expired run cannot be timed out", transitionErr)
			}
			tag, updateErr := db.Exec(ctx, `UPDATE runs SET state='TIMED_OUT',state_version=$3,updated_at=$4,terminal_at=$4,lease_id=NULL,lease_expires_at=NULL,fencing_token=$5 WHERE tenant_id=$1 AND id=$2 AND state_version=$6`, tenantID.String(), childRun, next.Version, now, fencingToken+1, runVersion)
			if updateErr != nil || tag.RowsAffected() != 1 {
				return fmt.Errorf("time out expired reservation child: %w", updateErr)
			}
			runState = string(domain.RunTimedOut)
			var sequence int64
			if err := db.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM run_events WHERE tenant_id=$1 AND run_id=$2`, tenantID.String(), childRun).Scan(&sequence); err != nil {
				return fmt.Errorf("sequence reconciliation timeout: %w", err)
			}
			payload, _ := json.Marshal(map[string]any{"reason": "reservation_expired", "reservation_id": reservationID, "state": runState, "state_version": next.Version})
			if _, err := db.Exec(ctx, `WITH inserted_event AS (INSERT INTO run_events(id,tenant_id,run_id,sequence,event_type,actor_type,actor_id,state,state_version,payload,occurred_at) VALUES($1,$2,$3,$4,'run.timed_out','SYSTEM',$5,'TIMED_OUT',$6,$7,$8) RETURNING id) INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) SELECT id,$2,'run',$3::text,'run.timed_out',1,$7,'{}'::jsonb,$8,$8 FROM inserted_event`, evidence.RunEventID.String(), tenantID.String(), childRun, sequence, systemPrincipalID.String(), next.Version, payload, now); err != nil {
				return fmt.Errorf("append reconciliation timeout event: %w", err)
			}
		}

		type lockedItem struct {
			dimension, name, unit          string
			scale                          int16
			reserved, available, allocated int64
		}
		rows, queryErr := db.Query(ctx, `SELECT i.dimension_id::text,d.name,i.unit,i.scale,i.reserved_value,c.available_value,c.allocated_open_value
FROM resource_reservation_items i
JOIN resource_dimensions d ON d.tenant_id=i.tenant_id AND d.id=i.dimension_id
JOIN resource_balances c ON c.tenant_id=i.tenant_id AND c.envelope_id=$3 AND c.dimension_id=i.dimension_id
JOIN resource_balances p ON p.tenant_id=i.tenant_id AND p.envelope_id=$4 AND p.dimension_id=i.dimension_id
WHERE i.tenant_id=$1 AND i.reservation_id=$2 ORDER BY i.dimension_id FOR UPDATE OF c,p`, tenantID.String(), reservationID, childEnvelope, parentEnvelope)
		if queryErr != nil {
			return fmt.Errorf("lock reconciliation balances: %w", queryErr)
		}
		var items []lockedItem
		for rows.Next() {
			var item lockedItem
			if err := rows.Scan(&item.dimension, &item.name, &item.unit, &item.scale, &item.reserved, &item.available, &item.allocated); err != nil {
				rows.Close()
				return err
			}
			items = append(items, item)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(items) == 0 {
			return domain.NewError(domain.CodeConflict, "reconciliation reservation has no consumable items")
		}
		for _, item := range items {
			if item.allocated != 0 || item.available > item.reserved {
				return domain.NewError(domain.CodeConflict, "reconciliation child balance is unsafe")
			}
		}

		parsedReservationID, parseErr := domain.ParseID(reservationID)
		if parseErr != nil {
			return parseErr
		}
		settlement := domain.ResourceSettlement{ID: evidence.SettlementID, TenantID: tenantID, ReservationID: parsedReservationID, ActorPrincipalID: systemPrincipalID, PolicyDecisionID: evidence.PolicyDecisionID, IdempotencyKey: "reconcile:" + reservationID, TerminalRunState: runState, SettledAt: now}
		digest := resourceSettlementDigest(settlement)
		reason, reservationState := "RECONCILED", "SETTLED"
		if expired {
			reason, reservationState = "EXPIRED", "EXPIRED_SETTLED"
		}
		if _, err := db.Exec(ctx, `INSERT INTO resource_settlements(id,tenant_id,reservation_id,actor_principal_id,policy_decision_id,idempotency_key,terminal_run_state,content_digest,reason,settled_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, evidence.SettlementID.String(), tenantID.String(), reservationID, systemPrincipalID.String(), evidence.PolicyDecisionID.String(), settlement.IdempotencyKey, runState, digest, reason, now); err != nil {
			return fmt.Errorf("insert reconciled settlement: %w", err)
		}
		for _, item := range items {
			consumed := item.reserved - item.available
			if _, err := db.Exec(ctx, `INSERT INTO resource_settlement_items(tenant_id,settlement_id,dimension_id,reserved_value,consumed_value,returned_value,unit,scale) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tenantID.String(), evidence.SettlementID.String(), item.dimension, item.reserved, consumed, item.available, item.unit, item.scale); err != nil {
				return fmt.Errorf("insert reconciled settlement item: %w", err)
			}
			tag, updateErr := db.Exec(ctx, `UPDATE resource_balances SET available_value=available_value+$4,allocated_open_value=allocated_open_value-$5,state_version=state_version+1,updated_at=$6 WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3 AND available_value<=9223372036854775807-$4 AND allocated_open_value >= $5 AND state_version<9223372036854775807`, tenantID.String(), parentEnvelope, item.dimension, item.available, item.reserved, now)
			if updateErr != nil || tag.RowsAffected() != 1 {
				return domain.NewError(domain.CodeConflict, "parent reconciliation balance changed or overflowed")
			}
		}
		if tag, err := db.Exec(ctx, `UPDATE resource_reservations SET state=$3,settled_at=$4 WHERE tenant_id=$1 AND id=$2 AND state='OPEN'`, tenantID.String(), reservationID, reservationState, now); err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("close reconciled reservation: %w", err)
		}

		payload, _ := json.Marshal(map[string]any{"reservation_id": reservationID, "settlement_id": evidence.SettlementID.String(), "reason": reason, "terminal_run_state": runState})
		reasons := json.RawMessage(`["resource.reservation.reconciled"]`)
		metadata, _ := json.Marshal(map[string]any{"expired": expired, "reconciliation_key": settlement.IdempotencyKey})
		tenant, actor, decision := tenantID, systemPrincipalID, evidence.PolicyDecisionID
		audit := AuditEvent{ID: evidence.AuditID, TenantID: &tenant, PrincipalID: &actor, Action: "resources.reconcile", ResourceType: "resource_reservation", ResourceID: reservationID, Outcome: "SUCCEEDED", ReasonCodes: reasons, PolicyDecisionID: &decision, Metadata: metadata, OccurredAt: now}
		message := OutboxMessage{ID: evidence.OutboxID, TenantID: &tenant, AggregateType: "resource_reservation", AggregateID: reservationID, EventType: "resource.reservation.reconciled", SchemaVersion: 1, Payload: payload, Headers: json.RawMessage(`{}`), OccurredAt: now, AvailableAt: now}
		if err := validateEvidence(audit, message); err != nil {
			return err
		}
		eventHash, err := hashAuditEvent(audit)
		if err != nil {
			return err
		}
		if _, err := db.Exec(ctx, `WITH inserted_audit AS (INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,reason_codes,policy_decision_id,metadata,event_hash,occurred_at) VALUES($1,$2,$3,'resources.reconcile','resource_reservation',$4,'SUCCEEDED',$5,$6,$7,$8,$9) RETURNING id) INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) SELECT $10,$2,'resource_reservation',$4,'resource.reservation.reconciled',1,$11,'{}'::jsonb,$9,$9 FROM inserted_audit`, evidence.AuditID.String(), tenantID.String(), systemPrincipalID.String(), reservationID, reasons, evidence.PolicyDecisionID.String(), metadata, eventHash, now, evidence.OutboxID.String(), payload); err != nil {
			return fmt.Errorf("append reconciliation evidence: %w", err)
		}
		tenantRepository := &TenantRepository{db: db, tenantID: tenantID}
		settlementResult := domain.ResourceSettlementResult{}
		if err := tenantRepository.loadSettlementResult(ctx, evidence.SettlementID.String(), reservationID, now, false, &settlementResult); err != nil {
			return err
		}
		result = domain.ResourceReconciliationResult{Settlement: settlementResult, Expired: expired}
		return nil
	})
	return result, err
}
