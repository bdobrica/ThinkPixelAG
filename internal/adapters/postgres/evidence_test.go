package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestEvidenceValidationAndHashAreDeterministic(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 19, 10, 0, 0, 123, time.UTC)
	tenant, principal := mustNewRepositoryID(t), mustNewRepositoryID(t)
	audit := AuditEvent{ID: mustNewRepositoryID(t), TenantID: &tenant, PrincipalID: &principal, Action: "agents.update", ResourceType: "agent", ResourceID: "one", Outcome: "SUCCEEDED", ReasonCodes: json.RawMessage(`[]`), Metadata: json.RawMessage(`{"safe":true}`), OccurredAt: now}
	message := OutboxMessage{ID: mustNewRepositoryID(t), TenantID: &tenant, AggregateType: "agent", AggregateID: "one", EventType: "agent.updated", SchemaVersion: 1, Payload: json.RawMessage(`{"id":"one"}`), Headers: json.RawMessage(`{}`), OccurredAt: now, AvailableAt: now}
	if err := validateEvidence(audit, message); err != nil {
		t.Fatal(err)
	}
	first, err := hashAuditEvent(audit)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hashAuditEvent(audit)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != len("sha256:")+64 {
		t.Fatalf("audit hashes = %q, %q", first, second)
	}
	message.Payload = json.RawMessage(`[]`)
	if err := validateEvidence(audit, message); err == nil {
		t.Fatal("accepted array outbox payload")
	}
}

func TestEvidenceTransactionValidatesBeforeStarting(t *testing.T) {
	t.Parallel()
	transactor := &Transactor{}
	err := transactor.WithinEvidenceTransaction(context.Background(), pgx.TxOptions{}, func(context.Context, DBTX) error { return errors.New("must not run") }, AuditEvent{}, OutboxMessage{})
	if err == nil {
		t.Fatal("invalid evidence accepted")
	}
}

func TestOutboxRetryDelayIsBoundedAndJittered(t *testing.T) {
	t.Parallel()
	p := &OutboxPublisher{config: OutboxConfig{BaseRetry: 2 * time.Second, MaxRetry: 10 * time.Second, Jitter: func(bound time.Duration) time.Duration { return bound }}}
	if got := p.retryDelay(1); got != 2*time.Second {
		t.Fatalf("first delay = %v", got)
	}
	if got := p.retryDelay(20); got != 10*time.Second {
		t.Fatalf("capped delay = %v", got)
	}
}

func TestOutboxPublisherConfiguration(t *testing.T) {
	t.Parallel()
	if _, err := NewOutboxPublisher(nil, nil, OutboxConfig{}); err == nil {
		t.Fatal("accepted empty config")
	}
}
