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

var _ ports.RunSignalRepository = (*TenantRepository)(nil)

func (r *TenantRepository) AppendRunSignal(ctx context.Context, signal domain.RunSignal, evidence ports.RunSignalEvidence) (domain.RunEvent, error) {
	if err := r.valid(); err != nil {
		return domain.RunEvent{}, err
	}
	if signal.TenantID != r.tenantID {
		return domain.RunEvent{}, errors.New("run signal does not match repository scope")
	}
	if err := signal.Validate(); err != nil {
		return domain.RunEvent{}, err
	}
	if evidence.EventID.IsZero() || evidence.AuditID.IsZero() || evidence.OutboxID.IsZero() || evidence.RequestID.IsZero() || evidence.PolicyDecisionID.IsZero() || len(evidence.ReasonCodes) == 0 {
		return domain.RunEvent{}, errors.New("run signal evidence is invalid")
	}
	var event domain.RunEvent
	err := r.withAdmissionTransaction(ctx, func(txr *TenantRepository) error {
		var state string
		var stateVersion int64
		if err := txr.db.QueryRow(ctx, `SELECT state,state_version FROM runs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, r.tenantID.String(), signal.RunID.String()).Scan(&state, &stateVersion); errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeNotFound, "run not found")
		} else if err != nil {
			return fmt.Errorf("lock signal run: %w", err)
		}
		if domain.RunState(state).Terminal() {
			return domain.NewError(domain.CodeConflict, "terminal run cannot accept signals")
		}
		if signal.ExpectedStateVersion != nil && *signal.ExpectedStateVersion != stateVersion {
			return domain.NewError(domain.CodeConflict, "run state version conflict")
		}
		var existingID string
		err := txr.db.QueryRow(ctx, `SELECT id::text FROM run_signals WHERE tenant_id=$1 AND run_id=$2 AND idempotency_key=$3`, r.tenantID.String(), signal.RunID.String(), signal.IdempotencyKey).Scan(&existingID)
		if err == nil {
			return txr.loadSignalEvent(ctx, signal.RunID, existingID, &event)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("query existing run signal: %w", err)
		}
		var sequence int64
		if err := txr.db.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM run_events WHERE tenant_id=$1 AND run_id=$2`, r.tenantID.String(), signal.RunID.String()).Scan(&sequence); err != nil {
			return fmt.Errorf("allocate run event sequence: %w", err)
		}
		eventPayload, _ := json.Marshal(map[string]any{"signal_id": signal.ID.String(), "signal_type": signal.Type, "payload": json.RawMessage(signal.Payload)})
		reasons, _ := json.Marshal(evidence.ReasonCodes)
		auditMetadata, _ := json.Marshal(map[string]any{"signal_id": signal.ID.String(), "signal_type": signal.Type, "event_sequence": sequence})
		outboxPayload, _ := json.Marshal(map[string]any{"run_id": signal.RunID.String(), "event_id": evidence.EventID.String(), "sequence": sequence, "signal_id": signal.ID.String(), "signal_type": signal.Type, "payload": json.RawMessage(signal.Payload)})
		tenant, principal, decision, request := signal.TenantID, signal.ActorPrincipalID, evidence.PolicyDecisionID, evidence.RequestID
		audit := AuditEvent{ID: evidence.AuditID, TenantID: &tenant, PrincipalID: &principal, Action: "runs.signal", ResourceType: "run", ResourceID: signal.RunID.String(), Outcome: "SUCCEEDED", ReasonCodes: reasons, PolicyDecisionID: &decision, RequestID: &request, Metadata: auditMetadata, OccurredAt: signal.CreatedAt}
		message := OutboxMessage{ID: evidence.OutboxID, TenantID: &tenant, AggregateType: "run", AggregateID: signal.RunID.String(), EventType: "run.signal.accepted", SchemaVersion: 1, Payload: outboxPayload, Headers: json.RawMessage(`{}`), OccurredAt: signal.CreatedAt, AvailableAt: signal.CreatedAt}
		if err := validateEvidence(audit, message); err != nil {
			return err
		}
		eventHash, err := hashAuditEvent(audit)
		if err != nil {
			return err
		}
		_, err = txr.db.Exec(ctx, `WITH inserted_signal AS (
 INSERT INTO run_signals(id,tenant_id,run_id,signal_type,payload,idempotency_key,actor_principal_id,created_at)
 VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8) RETURNING run_id
), inserted_event AS (
 INSERT INTO run_events(id,tenant_id,run_id,sequence,event_type,actor_type,actor_id,state,state_version,payload,occurred_at)
 SELECT $9,$2,$3,$10,'run.signal.accepted','CALLER',$7,$11,$12,$13::jsonb,$8 FROM inserted_signal RETURNING run_id
), inserted_audit AS (
 INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,reason_codes,policy_decision_id,request_id,metadata,event_hash,occurred_at)
 SELECT $14,$2,$7,'runs.signal','run',$3::text,'SUCCEEDED',$15::jsonb,$16,$17,$18::jsonb,$19,$8 FROM inserted_event RETURNING resource_id
)
INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at)
SELECT $20,$2,'run',$3::text,'run.signal.accepted',1,$21::jsonb,'{}'::jsonb,$8,$8 FROM inserted_audit`, signal.ID.String(), r.tenantID.String(), signal.RunID.String(), string(signal.Type), signal.Payload, signal.IdempotencyKey, signal.ActorPrincipalID.String(), signal.CreatedAt, evidence.EventID.String(), sequence, state, stateVersion, eventPayload, evidence.AuditID.String(), reasons, evidence.PolicyDecisionID.String(), evidence.RequestID.String(), auditMetadata, eventHash, evidence.OutboxID.String(), outboxPayload)
		if err != nil {
			return fmt.Errorf("append run signal evidence: %w", err)
		}
		event = domain.RunEvent{ID: evidence.EventID, RunID: signal.RunID, Sequence: sequence, Type: "run.signal.accepted", Data: map[string]any{"signal_id": signal.ID.String(), "signal_type": string(signal.Type)}, OccurredAt: signal.CreatedAt}
		return nil
	})
	return event, err
}

func (r *TenantRepository) loadSignalEvent(ctx context.Context, runID domain.ID, signalID string, event *domain.RunEvent) error {
	var id, rid, eventType string
	var payload []byte
	err := r.db.QueryRow(ctx, `SELECT id::text,run_id::text,sequence,event_type,payload,occurred_at FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND payload->>'signal_id'=$3`, r.tenantID.String(), runID.String(), signalID).Scan(&id, &rid, &event.Sequence, &eventType, &payload, &event.OccurredAt)
	if err != nil {
		return fmt.Errorf("load idempotent signal event: %w", err)
	}
	event.ID, _ = domain.ParseID(id)
	event.RunID, _ = domain.ParseID(rid)
	event.Type = eventType
	event.OccurredAt = event.OccurredAt.UTC()
	var data struct {
		SignalID   string `json:"signal_id"`
		SignalType string `json:"signal_type"`
	}
	if json.Unmarshal(payload, &data) != nil {
		return errors.New("stored signal event payload is invalid")
	}
	event.Data = map[string]any{"signal_id": data.SignalID, "signal_type": data.SignalType}
	return nil
}
