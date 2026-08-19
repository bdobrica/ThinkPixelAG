package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/jackc/pgx/v5"
)

var traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// AuditEvent is the immutable security evidence written with a domain mutation.
// Metadata must already be redacted and bounded by the calling use case.
type AuditEvent struct {
	ID               domain.ID
	TenantID         *domain.ID
	PrincipalID      *domain.ID
	Action           string
	ResourceType     string
	ResourceID       string
	Outcome          string
	ReasonCodes      json.RawMessage
	PolicyDecisionID *domain.ID
	RequestID        *domain.ID
	TraceID          string
	Metadata         json.RawMessage
	PreviousHash     string
	OccurredAt       time.Time
}

// OutboxMessage is a durable CloudEvents-like envelope. Payload and headers
// must be JSON objects; ID is the stable sink-side deduplication key.
type OutboxMessage struct {
	ID            domain.ID
	TenantID      *domain.ID
	AggregateType string
	AggregateID   string
	EventType     string
	SchemaVersion int
	Payload       json.RawMessage
	Headers       json.RawMessage
	OccurredAt    time.Time
	AvailableAt   time.Time
}

// WithinEvidenceTransaction commits a domain mutation, audit event and outbox
// message as one unit. Nothing is persisted when mutation or evidence fails.
func (t *Transactor) WithinEvidenceTransaction(ctx context.Context, options pgx.TxOptions, mutation TransactionFunc, audit AuditEvent, message OutboxMessage) error {
	if mutation == nil {
		return errors.New("evidence transaction requires a mutation")
	}
	if err := validateEvidence(audit, message); err != nil {
		return err
	}
	eventHash, err := hashAuditEvent(audit)
	if err != nil {
		return err
	}
	return t.WithinTransaction(ctx, options, func(ctx context.Context, tx DBTX) error {
		if err := mutation(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO audit_events
			(id, tenant_id, principal_id, action, resource_type, resource_id, outcome, reason_codes,
			 policy_decision_id, request_id, trace_id, metadata, previous_event_hash, event_hash, occurred_at)
			VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,NULLIF($11,''),$12,NULLIF($13,''),$14,$15)`,
			audit.ID.String(), optionalDBID(audit.TenantID), optionalDBID(audit.PrincipalID), audit.Action, audit.ResourceType,
			audit.ResourceID, audit.Outcome, []byte(audit.ReasonCodes), optionalDBID(audit.PolicyDecisionID), optionalDBID(audit.RequestID),
			audit.TraceID, []byte(audit.Metadata), audit.PreviousHash, eventHash, audit.OccurredAt); err != nil {
			return fmt.Errorf("insert audit event: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO outbox_messages
			(id, tenant_id, aggregate_type, aggregate_id, event_type, schema_version, payload, headers, occurred_at, available_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, message.ID.String(), optionalDBID(message.TenantID), message.AggregateType,
			message.AggregateID, message.EventType, message.SchemaVersion, []byte(message.Payload), []byte(message.Headers), message.OccurredAt, message.AvailableAt); err != nil {
			return fmt.Errorf("insert outbox message: %w", err)
		}
		return nil
	})
}

func validateEvidence(a AuditEvent, m OutboxMessage) error {
	if a.ID.IsZero() || m.ID.IsZero() {
		return errors.New("audit and outbox IDs are required")
	}
	if (a.TenantID == nil) != (m.TenantID == nil) || a.TenantID != nil && *a.TenantID != *m.TenantID {
		return errors.New("audit and outbox tenant scope must match")
	}
	if a.PrincipalID != nil && a.TenantID == nil {
		return errors.New("audit principal requires tenant scope")
	}
	if len(a.Action) < 1 || len(a.Action) > 128 || len(a.ResourceType) < 1 || len(a.ResourceType) > 128 || len(a.ResourceID) > 512 {
		return errors.New("audit action or resource is outside schema bounds")
	}
	if a.Outcome != "SUCCEEDED" && a.Outcome != "DENIED" && a.Outcome != "FAILED" {
		return errors.New("invalid audit outcome")
	}
	if a.TraceID != "" && !traceIDPattern.MatchString(a.TraceID) {
		return errors.New("invalid audit trace ID")
	}
	if a.PreviousHash != "" && !requestHashPattern.MatchString(a.PreviousHash) {
		return errors.New("invalid previous audit hash")
	}
	if !validJSONArray(a.ReasonCodes) || !validJSONObject(a.Metadata) || !validJSONObject(m.Payload) || !validJSONObject(m.Headers) {
		return errors.New("evidence JSON has an invalid shape")
	}
	if len(m.AggregateType) < 1 || len(m.AggregateType) > 128 || len(m.AggregateID) < 1 || len(m.AggregateID) > 512 || len(m.EventType) < 1 || len(m.EventType) > 256 || m.SchemaVersion < 1 {
		return errors.New("outbox envelope is outside schema bounds")
	}
	if _, err := domain.RequireUTC(a.OccurredAt); err != nil {
		return err
	}
	if _, err := domain.RequireUTC(m.OccurredAt); err != nil {
		return err
	}
	if _, err := domain.RequireUTC(m.AvailableAt); err != nil {
		return err
	}
	if m.AvailableAt.Before(m.OccurredAt) {
		return errors.New("outbox availability precedes occurrence")
	}
	return nil
}

func hashAuditEvent(a AuditEvent) (string, error) {
	canonical := struct {
		ID, TenantID, PrincipalID, Action, ResourceType, ResourceID, Outcome string
		ReasonCodes                                                          json.RawMessage
		PolicyDecisionID, RequestID, TraceID                                 string
		Metadata                                                             json.RawMessage
		PreviousHash, OccurredAt                                             string
	}{a.ID.String(), optionalID(a.TenantID), optionalID(a.PrincipalID), a.Action, a.ResourceType, a.ResourceID, a.Outcome,
		a.ReasonCodes, optionalID(a.PolicyDecisionID), optionalID(a.RequestID), a.TraceID, a.Metadata, a.PreviousHash, a.OccurredAt.Format(time.RFC3339Nano)}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode audit hash input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func optionalID(id *domain.ID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
func optionalDBID(id *domain.ID) any {
	if id == nil {
		return nil
	}
	return id.String()
}
func validJSONArray(value []byte) bool {
	var array []json.RawMessage
	return json.Unmarshal(value, &array) == nil && array != nil
}
