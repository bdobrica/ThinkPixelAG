package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdempotencyConcurrencyReplayConflictAndExpiry(t *testing.T) {
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
		connection.Close(ctx)
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		connection.Close(ctx)
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
	slug := "data009-" + tenant.String()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, slug, display_name, created_at, updated_at) VALUES ($1, $2, $2, $3, $3)`, tenant.String(), slug, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO principals (id, tenant_id, external_issuer, external_subject, principal_type, created_at) VALUES ($1, $2, 'https://data009.test', $3, 'HUMAN', $4)`, principal.String(), tenant.String(), principal.String(), now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM idempotency_records WHERE tenant_id = $1`, tenant.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM principals WHERE tenant_id = $1`, tenant.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = $1`, tenant.String())
	})

	repositories, err := NewRepositories(pool)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := repositories.ForTenant(tenant)
	if err != nil {
		t.Fatal(err)
	}
	request := IdempotencyRequest{
		PrincipalID: principal,
		Route:       "POST /v1/agents/{agent_id}/runs",
		Key:         "concurrent-key",
		RequestHash: HashIdempotencyRequest([]byte(`{"agent":"one"}`)),
		Lease:       5 * time.Second,
		TTL:         time.Hour,
	}

	const contenders = 16
	start := make(chan struct{})
	var acquired atomic.Int32
	var owner IdempotencyAcquisition
	var ownerMu sync.Mutex
	errorsSeen := make(chan error, contenders)
	var wait sync.WaitGroup
	for range contenders {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := repository.AcquireIdempotency(ctx, request, now)
			if err == nil {
				acquired.Add(1)
				ownerMu.Lock()
				owner = result
				ownerMu.Unlock()
				return
			}
			if !errors.Is(err, ErrIdempotencyInFlight) {
				errorsSeen <- err
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("concurrent acquisition: %v", err)
	}
	if got := acquired.Load(); got != 1 {
		t.Fatalf("owners = %d, want 1", got)
	}

	response := IdempotencyResponse{Status: 201, Headers: []byte(`{"Content-Type":["application/json"]}`), Body: []byte(`{"run_id":"stable"}`)}
	if err := repository.CompleteIdempotency(ctx, owner, response, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	replay, err := repository.AcquireIdempotency(ctx, request, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if replay.Outcome != IdempotencyReplay || replay.Response == nil || replay.Response.Status != response.Status || string(replay.Response.Body) != string(response.Body) {
		t.Fatalf("replay = %+v", replay)
	}

	conflict := request
	conflict.RequestHash = HashIdempotencyRequest([]byte(`{"agent":"two"}`))
	if _, err := repository.AcquireIdempotency(ctx, conflict, now.Add(3*time.Second)); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting acquisition error = %v", err)
	}

	expired := conflict
	expiredNow := now.Add(request.TTL + time.Second)
	replacement, err := repository.AcquireIdempotency(ctx, expired, expiredNow)
	if err != nil || replacement.Outcome != IdempotencyAcquired || replacement.RecordID == owner.RecordID {
		t.Fatalf("expired replacement = %+v, %v", replacement, err)
	}

	leaseRequest := request
	leaseRequest.Key = "abandoned-key"
	first, err := repository.AcquireIdempotency(ctx, leaseRequest, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.AcquireIdempotency(ctx, leaseRequest, now.Add(leaseRequest.Lease+time.Microsecond))
	if err != nil || second.RecordID == first.RecordID {
		t.Fatalf("lease replacement = %+v, %v", second, err)
	}
	if err := repository.CompleteIdempotency(ctx, first, response, now.Add(leaseRequest.Lease+time.Second)); !errors.Is(err, ErrIdempotencyOwnership) {
		t.Fatalf("stale owner completion error = %v", err)
	}
}
