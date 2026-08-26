package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.ResourceSettlementRepository = (*TenantRepository)(nil)

func (r *TenantRepository) GetSettlementTarget(ctx context.Context, reservationID domain.ID) (ports.ResourceSettlementTarget, error) {
	if err := r.valid(); err != nil {
		return ports.ResourceSettlementTarget{}, err
	}
	var runID string
	if err := r.db.QueryRow(ctx, `SELECT child_run_id::text FROM resource_reservations WHERE tenant_id=$1 AND id=$2`, r.tenantID.String(), reservationID.String()).Scan(&runID); errors.Is(err, pgx.ErrNoRows) {
		return ports.ResourceSettlementTarget{}, domain.NewError(domain.CodeNotFound, "reservation not found")
	} else if err != nil {
		return ports.ResourceSettlementTarget{}, fmt.Errorf("query settlement target: %w", err)
	}
	id, err := domain.ParseID(runID)
	if err != nil {
		return ports.ResourceSettlementTarget{}, fmt.Errorf("decode settlement target: %w", err)
	}
	record, err := r.GetRun(ctx, id)
	if err != nil {
		return ports.ResourceSettlementTarget{}, err
	}
	return ports.ResourceSettlementTarget{Run: record, ChildRunID: id}, nil
}

func (r *TenantRepository) SettleReservation(ctx context.Context, candidate domain.ResourceSettlement, evidence ports.ResourceSettlementEvidence) (result domain.ResourceSettlementResult, err error) {
	if err := r.valid(); err != nil {
		return result, err
	}
	settlement, err := domain.ValidateResourceSettlement(candidate)
	if err != nil {
		return result, err
	}
	if settlement.TenantID != r.tenantID || evidence.AuditID.IsZero() || evidence.OutboxID.IsZero() || evidence.RequestID.IsZero() || len(evidence.ReasonCodes) == 0 {
		return result, errors.New("resource settlement scope or evidence is invalid")
	}
	digest := resourceSettlementDigest(settlement)
	err = r.withResourceTransaction(ctx, func(txr *TenantRepository) error {
		lockKey := fmt.Sprintf("%s:%s:%d:%s", r.tenantID, settlement.ActorPrincipalID, len(settlement.IdempotencyKey), settlement.IdempotencyKey)
		if _, err := txr.db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
			return fmt.Errorf("lock settlement idempotency key: %w", err)
		}
		var existingID, existingReservation, existingDigest string
		var existingAt time.Time
		replayErr := txr.db.QueryRow(ctx, `SELECT id::text,reservation_id::text,content_digest,settled_at FROM resource_settlements WHERE tenant_id=$1 AND actor_principal_id=$2 AND idempotency_key=$3`, r.tenantID.String(), settlement.ActorPrincipalID.String(), settlement.IdempotencyKey).Scan(&existingID, &existingReservation, &existingDigest, &existingAt)
		if replayErr == nil {
			if existingDigest != digest {
				return domain.NewError(domain.CodeConflict, "settlement idempotency key was reused with different content")
			}
			return txr.loadSettlementResult(ctx, existingID, existingReservation, existingAt, true, &result)
		}
		if !errors.Is(replayErr, pgx.ErrNoRows) {
			return fmt.Errorf("query settlement replay: %w", replayErr)
		}
		var parentEnvelope, childEnvelope, childRun, state string
		err := txr.db.QueryRow(ctx, `SELECT parent_envelope_id::text,child_envelope_id::text,child_run_id::text,state FROM resource_reservations WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, r.tenantID.String(), settlement.ReservationID.String()).Scan(&parentEnvelope, &childEnvelope, &childRun, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeNotFound, "reservation not found")
		}
		if err != nil {
			return fmt.Errorf("lock reservation: %w", err)
		}
		if state != "OPEN" {
			var establishedID, establishedDigest string
			var establishedAt time.Time
			if err := txr.db.QueryRow(ctx, `SELECT id::text,content_digest,settled_at FROM resource_settlements WHERE tenant_id=$1 AND reservation_id=$2`, r.tenantID.String(), settlement.ReservationID.String()).Scan(&establishedID, &establishedDigest, &establishedAt); err != nil {
				return domain.NewError(domain.CodeConflict, "reservation is already closed")
			}
			if establishedDigest != digest {
				return domain.NewError(domain.CodeConflict, "reservation was settled with different content")
			}
			return txr.loadSettlementResult(ctx, establishedID, settlement.ReservationID.String(), establishedAt, true, &result)
		}
		var actualState string
		if err := txr.db.QueryRow(ctx, `SELECT state FROM runs JOIN principals p ON p.tenant_id=runs.tenant_id AND p.id=$3 AND p.disabled_at IS NULL WHERE runs.tenant_id=$1 AND runs.id=$2 FOR UPDATE OF runs`, r.tenantID.String(), childRun, settlement.ActorPrincipalID.String()).Scan(&actualState); errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeForbidden, "settlement actor or run is unavailable")
		} else if err != nil {
			return fmt.Errorf("lock settlement run: %w", err)
		}
		if actualState != settlement.TerminalRunState || !domain.RunState(actualState).Terminal() {
			return domain.NewError(domain.CodeConflict, "reservation child is not in the declared terminal state")
		}
		if len(settlement.FinalUsageEventIDs) > 0 {
			ids := make([]string, len(settlement.FinalUsageEventIDs))
			for i, id := range settlement.FinalUsageEventIDs {
				ids[i] = id.String()
			}
			var count int
			if err := txr.db.QueryRow(ctx, `SELECT count(*) FROM trusted_usage_entries WHERE tenant_id=$1 AND run_id=$2 AND id=ANY($3::uuid[])`, r.tenantID.String(), childRun, ids).Scan(&count); err != nil {
				return fmt.Errorf("validate final usage events: %w", err)
			}
			if count != len(ids) {
				return domain.NewError(domain.CodeConflict, "final usage event set contains an unknown event")
			}
		}
		type item struct {
			dimension, name, unit          string
			scale                          int16
			reserved, available, allocated int64
		}
		rows, err := txr.db.Query(ctx, `SELECT i.dimension_id::text,d.name,i.unit,i.scale,i.reserved_value,c.available_value,c.allocated_open_value FROM resource_reservation_items i JOIN resource_dimensions d ON d.tenant_id=i.tenant_id AND d.id=i.dimension_id JOIN resource_balances c ON c.tenant_id=i.tenant_id AND c.envelope_id=$3 AND c.dimension_id=i.dimension_id JOIN resource_balances p ON p.tenant_id=i.tenant_id AND p.envelope_id=$4 AND p.dimension_id=i.dimension_id WHERE i.tenant_id=$1 AND i.reservation_id=$2 ORDER BY i.dimension_id FOR UPDATE OF c,p`, r.tenantID.String(), settlement.ReservationID.String(), childEnvelope, parentEnvelope)
		if err != nil {
			return fmt.Errorf("lock settlement balances: %w", err)
		}
		var items []item
		for rows.Next() {
			var x item
			if err := rows.Scan(&x.dimension, &x.name, &x.unit, &x.scale, &x.reserved, &x.available, &x.allocated); err != nil {
				rows.Close()
				return err
			}
			items = append(items, x)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(items) == 0 {
			return domain.NewError(domain.CodeConflict, "reservation has no consumable items")
		}
		for _, x := range items {
			if x.allocated != 0 || x.available > x.reserved {
				return domain.NewError(domain.CodeConflict, "child has open allocations or an invalid balance")
			}
		}
		if _, err := txr.db.Exec(ctx, `INSERT INTO resource_settlements(id,tenant_id,reservation_id,actor_principal_id,policy_decision_id,idempotency_key,terminal_run_state,content_digest,reason,settled_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'TERMINAL',$9)`, settlement.ID.String(), r.tenantID.String(), settlement.ReservationID.String(), settlement.ActorPrincipalID.String(), settlement.PolicyDecisionID.String(), settlement.IdempotencyKey, settlement.TerminalRunState, digest, settlement.SettledAt); err != nil {
			return fmt.Errorf("insert settlement: %w", err)
		}
		for _, x := range items {
			consumed := x.reserved - x.available
			if _, err := txr.db.Exec(ctx, `INSERT INTO resource_settlement_items(tenant_id,settlement_id,dimension_id,reserved_value,consumed_value,returned_value,unit,scale) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, r.tenantID.String(), settlement.ID.String(), x.dimension, x.reserved, consumed, x.available, x.unit, x.scale); err != nil {
				return fmt.Errorf("insert settlement item: %w", err)
			}
			tag, err := txr.db.Exec(ctx, `UPDATE resource_balances SET available_value=available_value+$4,allocated_open_value=allocated_open_value-$5,state_version=state_version+1,updated_at=$6 WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3 AND available_value <= 9223372036854775807-$4 AND allocated_open_value >= $5 AND state_version < 9223372036854775807`, r.tenantID.String(), parentEnvelope, x.dimension, x.available, x.reserved, settlement.SettledAt)
			if err != nil || tag.RowsAffected() != 1 {
				return domain.NewError(domain.CodeConflict, "parent settlement balance changed or overflowed")
			}
		}
		if _, err := txr.db.Exec(ctx, `UPDATE resource_reservations SET state='SETTLED',settled_at=$3 WHERE tenant_id=$1 AND id=$2 AND state='OPEN'`, r.tenantID.String(), settlement.ReservationID.String(), settlement.SettledAt); err != nil {
			return fmt.Errorf("close reservation: %w", err)
		}
		payload, _ := json.Marshal(map[string]any{"reservation_id": settlement.ReservationID.String(), "settlement_id": settlement.ID.String(), "terminal_run_state": settlement.TerminalRunState})
		reasons, _ := json.Marshal(evidence.ReasonCodes)
		metadata, _ := json.Marshal(map[string]any{"idempotency_key": settlement.IdempotencyKey, "request_id": evidence.RequestID.String()})
		tenant, actor, decision, request := settlement.TenantID, settlement.ActorPrincipalID, settlement.PolicyDecisionID, evidence.RequestID
		audit := AuditEvent{ID: evidence.AuditID, TenantID: &tenant, PrincipalID: &actor, Action: "resources.settle", ResourceType: "resource_reservation", ResourceID: settlement.ReservationID.String(), Outcome: "SUCCEEDED", ReasonCodes: reasons, PolicyDecisionID: &decision, RequestID: &request, Metadata: metadata, OccurredAt: settlement.SettledAt}
		message := OutboxMessage{ID: evidence.OutboxID, TenantID: &tenant, AggregateType: "resource_reservation", AggregateID: settlement.ReservationID.String(), EventType: "resource.reservation.settled", SchemaVersion: 1, Payload: payload, Headers: json.RawMessage(`{}`), OccurredAt: settlement.SettledAt, AvailableAt: settlement.SettledAt}
		if err := validateEvidence(audit, message); err != nil {
			return err
		}
		eventHash, err := hashAuditEvent(audit)
		if err != nil {
			return err
		}
		if _, err := txr.db.Exec(ctx, `INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,reason_codes,policy_decision_id,request_id,metadata,event_hash,occurred_at) VALUES($1,$2,$3,'resources.settle','resource_reservation',$4,'SUCCEEDED',$5,$6,$7,$8,$9,$10)`, evidence.AuditID.String(), r.tenantID.String(), settlement.ActorPrincipalID.String(), settlement.ReservationID.String(), reasons, settlement.PolicyDecisionID.String(), evidence.RequestID.String(), metadata, eventHash, settlement.SettledAt); err != nil {
			return fmt.Errorf("append settlement audit: %w", err)
		}
		if _, err := txr.db.Exec(ctx, `INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) VALUES($1,$2,'resource_reservation',$3,'resource.reservation.settled',1,$4,'{}'::jsonb,$5,$5)`, evidence.OutboxID.String(), r.tenantID.String(), settlement.ReservationID.String(), payload, settlement.SettledAt); err != nil {
			return fmt.Errorf("append settlement outbox: %w", err)
		}
		return txr.loadSettlementResult(ctx, settlement.ID.String(), settlement.ReservationID.String(), settlement.SettledAt, false, &result)
	})
	return result, err
}

func (r *TenantRepository) loadSettlementResult(ctx context.Context, settlementID, reservationID string, at time.Time, duplicate bool, result *domain.ResourceSettlementResult) error {
	id, err := domain.ParseID(settlementID)
	if err != nil {
		return err
	}
	rid, err := domain.ParseID(reservationID)
	if err != nil {
		return err
	}
	rows, err := r.db.Query(ctx, `SELECT d.name,i.unit,i.scale,i.consumed_value,i.returned_value FROM resource_settlement_items i JOIN resource_dimensions d ON d.tenant_id=i.tenant_id AND d.id=i.dimension_id WHERE i.tenant_id=$1 AND i.settlement_id=$2 ORDER BY d.name`, r.tenantID.String(), settlementID)
	if err != nil {
		return err
	}
	defer rows.Close()
	*result = domain.ResourceSettlementResult{ID: id, ReservationID: rid, SettledAt: at.UTC(), Duplicate: duplicate}
	for rows.Next() {
		var name, unit string
		var scale int16
		var consumed, returned int64
		if err := rows.Scan(&name, &unit, &scale, &consumed, &returned); err != nil {
			return err
		}
		result.Consumed = append(result.Consumed, domain.ResourceSettlementAmount{Name: name, Unit: unit, Scale: uint8(scale), Value: consumed})
		result.Returned = append(result.Returned, domain.ResourceSettlementAmount{Name: name, Unit: unit, Scale: uint8(scale), Value: returned})
	}
	return rows.Err()
}

func resourceSettlementDigest(s domain.ResourceSettlement) string {
	ids := make([]string, len(s.FinalUsageEventIDs))
	for i, id := range s.FinalUsageEventIDs {
		ids[i] = id.String()
	}
	sort.Strings(ids)
	b := strings.Join([]string{s.ReservationID.String(), s.TerminalRunState, strings.Join(ids, ",")}, "\x00")
	sum := sha256.Sum256([]byte(b))
	return "sha256:" + hex.EncodeToString(sum[:])
}
