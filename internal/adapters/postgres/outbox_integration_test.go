package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTransactionalEvidenceAndReplaySafeOutbox(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := NewMigrator(ctx, connection, os.DirFS(projectMigrationsDir(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tenant, principal := mustNewRepositoryID(t), mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id,slug,display_name,created_at,updated_at) VALUES ($1,$2,'before',$3,$3)`, tenant.String(), "data010-"+tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO principals (id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES ($1,$2,'https://data010.test',$3,'WORKLOAD',$4)`, principal.String(), tenant.String(), principal.String(), now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE tenant_id=$1`, tenant.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM audit_events WHERE tenant_id=$1`, tenant.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM principals WHERE tenant_id=$1`, tenant.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id=$1`, tenant.String())
	})

	transactor, err := NewTransactor(pool)
	if err != nil {
		t.Fatal(err)
	}
	auditID, messageID := mustNewRepositoryID(t), mustNewRepositoryID(t)
	audit := AuditEvent{ID: auditID, TenantID: &tenant, PrincipalID: &principal, Action: "tenant.update", ResourceType: "tenant", ResourceID: tenant.String(), Outcome: "SUCCEEDED", ReasonCodes: json.RawMessage(`[]`), Metadata: json.RawMessage(`{}`), OccurredAt: now}
	message := OutboxMessage{ID: messageID, TenantID: &tenant, AggregateType: "tenant", AggregateID: tenant.String(), EventType: "tenant.updated", SchemaVersion: 1, Payload: json.RawMessage(`{"display_name":"after"}`), Headers: json.RawMessage(`{}`), OccurredAt: now, AvailableAt: now}
	rollback := errors.New("rollback")
	err = transactor.WithinEvidenceTransaction(ctx, pgx.TxOptions{}, func(ctx context.Context, tx DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE tenants SET display_name='not-committed' WHERE id=$1`, tenant.String())
		if err != nil {
			return err
		}
		return rollback
	}, audit, message)
	if !errors.Is(err, rollback) {
		t.Fatalf("rollback error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE id=$1`, auditID.String()).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rolled back audit count=%d err=%v", count, err)
	}
	if err := transactor.WithinEvidenceTransaction(ctx, pgx.TxOptions{}, func(ctx context.Context, tx DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE tenants SET display_name='after' WHERE id=$1`, tenant.String())
		return err
	}, audit, message); err != nil {
		t.Fatal(err)
	}
	var display, hash string
	if err := pool.QueryRow(ctx, `SELECT t.display_name,a.event_hash FROM tenants t JOIN audit_events a ON a.tenant_id=t.id WHERE t.id=$1`, tenant.String()).Scan(&display, &hash); err != nil || display != "after" || len(hash) != 71 {
		t.Fatalf("committed evidence display=%q hash=%q err=%v", display, hash, err)
	}

	sink := &recordingSink{}
	config := OutboxConfig{WorkerID: "data010-worker", BatchSize: 1, MaxAttempts: 3, Lease: time.Minute, BaseRetry: time.Second, MaxRetry: time.Minute, Jitter: func(time.Duration) time.Duration { return 0 }}
	publishers := make([]*OutboxPublisher, 8)
	for i := range publishers {
		publishers[i], err = NewOutboxPublisher(pool, sink, config)
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errorsSeen := make(chan error, len(publishers))
	for _, publisher := range publishers {
		wg.Add(1)
		go func() { defer wg.Done(); _, publishErr := publisher.PublishBatch(ctx, now); errorsSeen <- publishErr }()
	}
	wg.Wait()
	close(errorsSeen)
	for publishErr := range errorsSeen {
		if publishErr != nil {
			t.Fatalf("concurrent publish: %v", publishErr)
		}
	}
	if sink.count(messageID) != 1 {
		t.Fatalf("concurrent calls for message=%d want 1", sink.count(messageID))
	}
	var published *time.Time
	if err := pool.QueryRow(ctx, `SELECT published_at FROM outbox_messages WHERE id=$1`, messageID.String()).Scan(&published); err != nil || published == nil {
		t.Fatalf("published_at=%v err=%v", published, err)
	}

	retryID := insertOutboxFixture(t, ctx, pool, tenant, now, "retry")
	failing := &recordingSink{err: &PublishError{Code: "unavailable", Err: errors.New("down")}}
	retryConfig := config
	retryConfig.BatchSize = 1000
	retryPublisher, _ := NewOutboxPublisher(pool, failing, retryConfig)
	if _, err := retryPublisher.PublishBatch(ctx, now); err != nil {
		t.Fatalf("retry publish: %v", err)
	}
	var available time.Time
	var code string
	if err := pool.QueryRow(ctx, `SELECT available_at,last_error_code FROM outbox_messages WHERE id=$1`, retryID.String()).Scan(&available, &code); err != nil || !available.After(now) || code != "unavailable" {
		t.Fatalf("retry state=%v,%q,%v", available, code, err)
	}

	poisonID := insertOutboxFixture(t, ctx, pool, tenant, now, "poison")
	poison := &recordingSink{err: &PublishError{Code: "invalid_payload", Permanent: true, Err: errors.New("reject")}}
	poisonPublisher, _ := NewOutboxPublisher(pool, poison, retryConfig)
	if _, err := poisonPublisher.PublishBatch(ctx, now); err != nil {
		t.Fatal(err)
	}
	var dead *time.Time
	if err := pool.QueryRow(ctx, `SELECT dead_lettered_at FROM outbox_messages WHERE id=$1`, poisonID.String()).Scan(&dead); err != nil || dead == nil {
		t.Fatalf("dead_lettered_at=%v err=%v", dead, err)
	}
}

type recordingSink struct {
	calls atomic.Int32
	mu    sync.Mutex
	byID  map[domain.ID]int
	err   error
}

func (s *recordingSink) Send(_ context.Context, message OutboxMessage) error {
	s.calls.Add(1)
	s.mu.Lock()
	if s.byID == nil {
		s.byID = make(map[domain.ID]int)
	}
	s.byID[message.ID]++
	s.mu.Unlock()
	return s.err
}

func (s *recordingSink) count(id domain.ID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byID[id]
}

func insertOutboxFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant domain.ID, now time.Time, suffix string) domain.ID {
	t.Helper()
	id := mustNewRepositoryID(t)
	_, err := pool.Exec(ctx, `INSERT INTO outbox_messages (id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) VALUES ($1,$2,'test',$3,$4,1,'{}','{}',$5,$5)`, id.String(), tenant.String(), suffix, "test."+suffix, now)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
