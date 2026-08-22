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

var _ ports.RunCancellationRepository = (*TenantRepository)(nil)

func (r *TenantRepository) CancelRun(ctx context.Context, cancellation domain.RunCancellation, evidence ports.RunCancellationEvidence) (domain.Run, error) {
	if err := r.valid(); err != nil {
		return domain.Run{}, err
	}
	if cancellation.TenantID != r.tenantID {
		return domain.Run{}, errors.New("run cancellation does not match repository scope")
	}
	if err := cancellation.Validate(); err != nil {
		return domain.Run{}, err
	}
	if evidence.SignalID.IsZero() || evidence.EventID.IsZero() || evidence.AuditID.IsZero() || evidence.OutboxID.IsZero() || evidence.RequestID.IsZero() || evidence.PolicyDecisionID.IsZero() || len(evidence.ReasonCodes) == 0 {
		return domain.Run{}, errors.New("run cancellation evidence is invalid")
	}
	err := r.withAdmissionTransaction(ctx, func(txr *TenantRepository) error {
		var state string
		var stateVersion, fencingToken int64
		var updatedAt time.Time
		var terminalAt *time.Time
		if err := txr.db.QueryRow(ctx, `SELECT state,state_version,updated_at,terminal_at,fencing_token FROM runs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, r.tenantID.String(), cancellation.RunID.String()).Scan(&state, &stateVersion, &updatedAt, &terminalAt, &fencingToken); errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeNotFound, "run not found")
		} else if err != nil {
			return fmt.Errorf("lock cancellation run: %w", err)
		}
		// A terminal transition that won the row lock is authoritative. Returning
		// it without another event makes cancellation safe against completion,
		// failure, timeout, and duplicate-cancel races.
		current := domain.RunLifecycle{State: domain.RunState(state), Version: stateVersion, UpdatedAt: updatedAt.UTC(), TerminalAt: terminalAt}
		if current.State.Terminal() {
			return nil
		}
		expected := stateVersion
		if cancellation.ExpectedStateVersion != nil {
			expected = *cancellation.ExpectedStateVersion
		}
		next, changed, transitionErr := current.Transition(domain.RunTransition{To: domain.RunCancelled, Actor: domain.RunActorCaller, ExpectedVersion: expected, At: cancellation.CreatedAt})
		if transitionErr != nil {
			switch {
			case errors.Is(transitionErr, domain.ErrRunVersionConflict):
				return domain.NewError(domain.CodeConflict, "run state version conflict")
			default:
				return domain.NewError(domain.CodeConflict, "run cannot be cancelled from its current state")
			}
		}
		if !changed {
			return nil
		}
		if fencingToken == int64(^uint64(0)>>1) {
			return domain.NewError(domain.CodeConflict, "run fencing token is exhausted")
		}
		var sequence int64
		if err := txr.db.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM run_events WHERE tenant_id=$1 AND run_id=$2`, r.tenantID.String(), cancellation.RunID.String()).Scan(&sequence); err != nil {
			return fmt.Errorf("allocate cancellation event sequence: %w", err)
		}
		payload, _ := json.Marshal(map[string]any{"reason_code": cancellation.ReasonCode})
		eventPayload, _ := json.Marshal(map[string]any{"reason_code": cancellation.ReasonCode, "previous_state": state})
		reasons, _ := json.Marshal(evidence.ReasonCodes)
		auditMetadata, _ := json.Marshal(map[string]any{"previous_state": state, "state": next.State, "state_version": next.Version, "event_sequence": sequence, "reason_code": cancellation.ReasonCode})
		outboxPayload, _ := json.Marshal(map[string]any{"run_id": cancellation.RunID.String(), "event_id": evidence.EventID.String(), "sequence": sequence, "previous_state": state, "state": next.State, "state_version": next.Version, "reason_code": cancellation.ReasonCode})
		tenant, principal, decision, request := cancellation.TenantID, cancellation.ActorPrincipalID, evidence.PolicyDecisionID, evidence.RequestID
		audit := AuditEvent{ID: evidence.AuditID, TenantID: &tenant, PrincipalID: &principal, Action: "runs.cancel", ResourceType: "run", ResourceID: cancellation.RunID.String(), Outcome: "SUCCEEDED", ReasonCodes: reasons, PolicyDecisionID: &decision, RequestID: &request, Metadata: auditMetadata, OccurredAt: cancellation.CreatedAt}
		message := OutboxMessage{ID: evidence.OutboxID, TenantID: &tenant, AggregateType: "run", AggregateID: cancellation.RunID.String(), EventType: "run.cancelled", SchemaVersion: 1, Payload: outboxPayload, Headers: json.RawMessage(`{}`), OccurredAt: cancellation.CreatedAt, AvailableAt: cancellation.CreatedAt}
		if err := validateEvidence(audit, message); err != nil {
			return err
		}
		eventHash, err := hashAuditEvent(audit)
		if err != nil {
			return err
		}
		_, err = txr.db.Exec(ctx, `WITH updated_run AS (
 UPDATE runs SET state='CANCELLED',state_version=$4,updated_at=$5,terminal_at=$5,lease_id=NULL,lease_expires_at=NULL,fencing_token=fencing_token+1
 WHERE tenant_id=$1 AND id=$2 AND state_version=$3 AND state=$6 RETURNING id
), inserted_signal AS (
 INSERT INTO run_signals(id,tenant_id,run_id,signal_type,payload,idempotency_key,actor_principal_id,created_at)
 SELECT $7,$1,$2,'CANCEL',$8::jsonb,$9,$10,$5 FROM updated_run RETURNING run_id
), inserted_event AS (
 INSERT INTO run_events(id,tenant_id,run_id,sequence,event_type,actor_type,actor_id,state,state_version,payload,occurred_at)
 SELECT $11,$1,$2,$12,'run.cancelled','CALLER',$10,'CANCELLED',$4,$13::jsonb,$5 FROM inserted_signal RETURNING run_id
), inserted_audit AS (
 INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,reason_codes,policy_decision_id,request_id,metadata,event_hash,occurred_at)
 SELECT $14,$1,$10,'runs.cancel','run',$2::text,'SUCCEEDED',$15::jsonb,$16,$17,$18::jsonb,$19,$5 FROM inserted_event RETURNING resource_id
)
INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at)
SELECT $20,$1,'run',$2::text,'run.cancelled',1,$21::jsonb,'{}'::jsonb,$5,$5 FROM inserted_audit`, r.tenantID.String(), cancellation.RunID.String(), stateVersion, next.Version, cancellation.CreatedAt, state, evidence.SignalID.String(), payload, cancellation.IdempotencyKey, cancellation.ActorPrincipalID.String(), evidence.EventID.String(), sequence, eventPayload, evidence.AuditID.String(), reasons, evidence.PolicyDecisionID.String(), evidence.RequestID.String(), auditMetadata, eventHash, evidence.OutboxID.String(), outboxPayload)
		if err != nil {
			return fmt.Errorf("cancel run with evidence: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Run{}, err
	}
	record, err := r.GetRun(ctx, cancellation.RunID)
	if err != nil {
		return domain.Run{}, err
	}
	return record.Run, nil
}
