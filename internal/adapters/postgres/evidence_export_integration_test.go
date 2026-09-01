package postgres

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestEvidenceDeliveryReplayReceiptAndCheckpoint(t *testing.T) {
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
	if err = migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close(ctx)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	now := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	tenant, event1, event2 := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
	sinkID := "sec006-" + tenant.String()
	if _, err = tx.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,'evidence',$3,$3)`, tenant.String(), "sec006-"+tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{event1.String(), event2.String()} {
		if _, err = tx.Exec(ctx, `INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) VALUES($1,$2,'security',$5,'evidence',1,$3,'{}',$4,$4)`, id, tenant.String(), []byte(`{"type":"POLICY"}`), now.Add(time.Duration(index)*time.Microsecond), id); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewEvidenceDeliveryStore(tx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Claim(ctx, sinkID, now)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.Claim(ctx, sinkID, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if first.Delivery.EventID != replay.Delivery.EventID || first.Delivery.EventHash != replay.Delivery.EventHash || first.ClaimToken == replay.ClaimToken {
		t.Fatal("lease replay did not preserve delivery identity with fresh fencing")
	}
	receipt := evidence.Receipt{Version: evidence.DeliveryVersion, SinkID: sinkID, Sequence: 1, EventID: replay.Delivery.EventID, EventHash: replay.Delivery.EventHash, ReceiptID: "receipt-1", Checkpoint: replay.Delivery.EventHash, AcceptedAt: now.Add(2 * time.Second)}
	if err = store.Complete(ctx, *replay, receipt); err != nil {
		t.Fatal(err)
	}
	if err = store.Complete(ctx, *first, receipt); err == nil {
		t.Fatal("stale claimant committed receipt")
	}
	next, err := store.Claim(ctx, sinkID, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if next.Delivery.Sequence != 2 || next.Delivery.PreviousHash != replay.Delivery.EventHash || next.Delivery.EventID != event2.String() {
		t.Fatalf("next delivery=%+v", next.Delivery)
	}
	var sequence int64
	var hash string
	var raw json.RawMessage
	if err = tx.QueryRow(ctx, `SELECT last_sequence,last_event_hash FROM evidence_sink_checkpoints WHERE sink_id=$1`, sinkID).Scan(&sequence, &hash); err != nil || sequence != 1 || hash != replay.Delivery.EventHash {
		t.Fatalf("checkpoint=%d,%s err=%v", sequence, hash, err)
	}
	if err = tx.QueryRow(ctx, `SELECT receipt FROM evidence_delivery_receipts WHERE sink_id=$1 AND event_id=$2`, sinkID, event1.String()).Scan(&raw); err != nil || !json.Valid(raw) {
		t.Fatalf("receipt=%s err=%v", raw, err)
	}
}
