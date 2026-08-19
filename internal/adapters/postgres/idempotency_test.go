package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestHashIdempotencyRequest(t *testing.T) {
	t.Parallel()
	got := HashIdempotencyRequest([]byte(`{"a":1}`))
	const want = "sha256:015abd7f5cc57a2dd94b7590f04ad8084273905ee33ec5cebeae62276a97f862"
	if got != want {
		t.Fatalf("HashIdempotencyRequest = %q, want %q", got, want)
	}
}

func TestAcquireIdempotencyValidatesBeforeQuery(t *testing.T) {
	t.Parallel()
	tenant := mustNewRepositoryID(t)
	repositories, err := NewRepositories(&idempotencyDB{})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := repositories.ForTenant(tenant)
	if err != nil {
		t.Fatal(err)
	}
	request := IdempotencyRequest{PrincipalID: mustNewRepositoryID(t), Route: "/v1/runs", Key: "key", RequestHash: "not-a-hash", Lease: time.Second, TTL: time.Minute}
	if _, err := repository.AcquireIdempotency(context.Background(), request, time.Now().UTC()); err == nil {
		t.Fatal("AcquireIdempotency accepted a malformed request hash")
	}
}

func TestScanIdempotencyOutcomes(t *testing.T) {
	t.Parallel()
	id, owner := mustNewRepositoryID(t), mustNewRepositoryID(t)
	hash := HashIdempotencyRequest([]byte("request"))
	status := 201
	replay, err := scanIdempotency(idempotencyRow{values: []any{id.String(), hash, "COMPLETED", owner.String(), &status, []byte(`{"Content-Type":["application/json"]}`), []byte(`{"id":"one"}`)}}, hash, false)
	if err != nil || replay.Outcome != IdempotencyReplay || replay.Response == nil || replay.Response.Status != 201 {
		t.Fatalf("completed scan = %+v, %v", replay, err)
	}
	_, err = scanIdempotency(idempotencyRow{values: []any{id.String(), hash, "IN_PROGRESS", owner.String(), (*int)(nil), []byte(nil), []byte(nil)}}, hash, false)
	if !errors.Is(err, ErrIdempotencyInFlight) {
		t.Fatalf("in-flight scan error = %v", err)
	}
	_, err = scanIdempotency(idempotencyRow{values: []any{id.String(), hash, "COMPLETED", owner.String(), &status, []byte(json.RawMessage(`{}`)), []byte(nil)}}, HashIdempotencyRequest([]byte("different")), false)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("hash mismatch error = %v", err)
	}
}

type idempotencyDB struct{}

func (*idempotencyDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}
func (*idempotencyDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (*idempotencyDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return idempotencyRow{err: errors.New("unexpected QueryRow")}
}

type idempotencyRow struct {
	values []any
	err    error
}

func (r idempotencyRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, value := range r.values {
		switch pointer := dest[i].(type) {
		case *string:
			*pointer = value.(string)
		case **int:
			*pointer = value.(*int)
		case *[]byte:
			*pointer = append((*pointer)[:0], value.([]byte)...)
		default:
			return errors.New("unexpected scan destination")
		}
	}
	return nil
}
