package postgres

import (
	"context"
	"encoding/json"
	"errors"
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
	events := []string{event1.String(), event2.String()}
	for range 62 {
		events = append(events, mustNewRepositoryID(t).String())
	}
	for index, id := range events {
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
	other, _ := NewEvidenceDeliveryStore(tx, time.Second)
	if competing, err := other.Claim(ctx, sinkID, now.Add(time.Millisecond)); err != nil || competing != nil {
		t.Fatalf("active lease was not exclusive: claim=%v error=%v", competing, err)
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
	var published *time.Time
	if err = tx.QueryRow(ctx, `SELECT published_at FROM outbox_messages WHERE id=$1`, event1.String()).Scan(&published); err != nil || published == nil || !published.Equal(receipt.AcceptedAt) {
		t.Fatalf("receipt did not atomically mark publication: %v", err)
	}
	var unpublished *time.Time
	if err = tx.QueryRow(ctx, `SELECT published_at FROM outbox_messages WHERE id=$1`, event2.String()).Scan(&unpublished); err != nil || unpublished != nil {
		t.Fatalf("undelivered event marked published: %v", err)
	}
	if err = store.Complete(ctx, *first, receipt); err == nil {
		t.Fatal("stale claimant committed receipt")
	}
	next, err := other.Claim(ctx, sinkID, now.Add(3*time.Second))
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
	// Remote acceptance followed by a lost response must remain replayable even
	// when an earlier-dated event commits before the retry or the process restarts.
	late := mustNewRepositoryID(t)
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) VALUES($1,$2,'security',$1::uuid::text,'evidence',1,'{"type":"POLICY"}','{}',$3,$3)`, late.String(), tenant.String(), now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := other.Release(ctx, *next); err != nil {
		t.Fatal(err)
	}
	releasedReceipt := evidence.Receipt{Version: evidence.DeliveryVersion, SinkID: sinkID, Sequence: next.Delivery.Sequence, EventID: next.Delivery.EventID, EventHash: next.Delivery.EventHash, ReceiptID: "released-owner", Checkpoint: next.Delivery.EventHash, AcceptedAt: now.Add(4 * time.Second)}
	if err := other.Complete(ctx, *next, releasedReceipt); !errors.Is(err, ErrOutboxClaimLost) {
		t.Fatalf("release did not immediately fence ownership: %v", err)
	}
	restarted, _ := NewEvidenceDeliveryStore(tx, time.Second)
	retried, err := restarted.Claim(ctx, sinkID, now.Add(4*time.Second))
	if err != nil || retried == nil || (retried.Delivery.EventHash != next.Delivery.EventHash || retried.Delivery.EventID != next.Delivery.EventID) {
		t.Fatalf("released delivery changed after restart: claim=%v error=%v", retried, err)
	}
	if retried.ClaimToken == next.ClaimToken {
		t.Fatal("retry did not fence the old owner")
	}
	// A new process after lease expiry must preserve the same pending delivery.
	expiredStore, _ := NewEvidenceDeliveryStore(tx, time.Second)
	expired, err := expiredStore.Claim(ctx, sinkID, now.Add(6*time.Second))
	if err != nil || expired == nil || expired.Delivery.EventHash != next.Delivery.EventHash || expired.Delivery.EventID != next.Delivery.EventID {
		t.Fatalf("expired delivery changed: claim=%v error=%v", expired, err)
	}
	receipt2 := evidence.Receipt{Version: evidence.DeliveryVersion, SinkID: sinkID, Sequence: expired.Delivery.Sequence, EventID: expired.Delivery.EventID, EventHash: expired.Delivery.EventHash, ReceiptID: "receipt-2", Checkpoint: expired.Delivery.EventHash, AcceptedAt: now.Add(6 * time.Second)}
	if err := restarted.Complete(ctx, *retried, receipt2); err == nil {
		t.Fatal("expired owner completed the delivery")
	}
	if err := expiredStore.Complete(ctx, *expired, receipt2); err != nil {
		t.Fatal(err)
	}
	lastSequence, lastHash := expired.Delivery.Sequence, expired.Delivery.EventHash
	seen := map[string]bool{event1.String(): true, event2.String(): true}
	foundLate := false
	// This store still has hints from before the other exporters completed the
	// two original events. Those hints must neither duplicate delivery nor
	// indefinitely hide a newly committed event.
	for range 65 {
		claim, err := store.Claim(ctx, sinkID, now.Add(7*time.Second))
		if err != nil || claim == nil {
			t.Fatalf("pending delivery unavailable: claim=%v error=%v", claim, err)
		}
		if seen[claim.Delivery.EventID] || claim.Delivery.Sequence != lastSequence+1 || claim.Delivery.PreviousHash != lastHash {
			t.Fatal("stale hints duplicated delivery or broke the hash chain")
		}
		seen[claim.Delivery.EventID] = true
		r := evidence.Receipt{Version: evidence.DeliveryVersion, SinkID: sinkID, Sequence: claim.Delivery.Sequence, EventID: claim.Delivery.EventID, EventHash: claim.Delivery.EventHash, ReceiptID: mustNewRepositoryID(t).String(), Checkpoint: claim.Delivery.EventHash, AcceptedAt: now.Add(7 * time.Second)}
		if err := store.Complete(ctx, *claim, r); err != nil {
			t.Fatal(err)
		}
		lastSequence, lastHash = claim.Delivery.Sequence, claim.Delivery.EventHash
		if claim.Delivery.EventID == late.String() {
			foundLate = true
			break
		}
	}
	if !foundLate {
		t.Fatal("bounded candidate prefetch starved a newly committed event")
	}
}
