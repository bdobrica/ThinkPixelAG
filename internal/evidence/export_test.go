package evidence

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestDeliverySchemaIsClosedAndVersioned(t *testing.T) {
	raw, err := os.ReadFile("../../api/schemas/evidence-delivery-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err = json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["additionalProperties"] != false {
		t.Fatal("delivery schema is not closed")
	}
	properties := schema["properties"].(map[string]any)
	version := properties["version"].(map[string]any)
	if version["const"] != DeliveryVersion {
		t.Fatalf("schema version=%v", version["const"])
	}
}

type exportStore struct {
	claim     *ClaimedDelivery
	completed int
	released  int
}

func (s *exportStore) Claim(context.Context, string, time.Time) (*ClaimedDelivery, error) {
	return s.claim, nil
}
func (s *exportStore) Complete(_ context.Context, _ ClaimedDelivery, _ Receipt) error {
	s.completed++
	return nil
}
func (s *exportStore) Release(_ context.Context, _ ClaimedDelivery) error { s.released++; return nil }

type exportSink struct {
	receipt Receipt
	calls   int
}

func (s *exportSink) Export(_ context.Context, _ Delivery) (Receipt, error) {
	s.calls++
	return s.receipt, nil
}

func TestDeliveryHashLinkAndReceiptBinding(t *testing.T) {
	first, err := NewDelivery("independent-compliance", 1, "", "01900000-0000-7000-8000-000000000001", json.RawMessage(`{"version":"thinkpixelag.evidence/v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	replay, _ := NewDelivery(first.SinkID, first.Sequence, first.PreviousHash, first.EventID, first.Event)
	if replay.EventHash != first.EventHash {
		t.Fatal("replay hash changed")
	}
	second, err := NewDelivery(first.SinkID, 2, first.EventHash, "01900000-0000-7000-8000-000000000002", json.RawMessage(`{"version":"thinkpixelag.evidence/v1"}`))
	if err != nil || second.PreviousHash != first.EventHash {
		t.Fatalf("link=%q err=%v", second.PreviousHash, err)
	}
	receipt := Receipt{DeliveryVersion, first.SinkID, 1, first.EventID, first.EventHash, "opaque-receipt", first.EventHash, time.Now().UTC()}
	if err := receipt.ValidateFor(first); err != nil {
		t.Fatal(err)
	}
	receipt.EventID = second.EventID
	if receipt.ValidateFor(first) == nil {
		t.Fatal("substituted receipt accepted")
	}
}

func TestExporterPersistsValidatedReceipt(t *testing.T) {
	d, _ := NewDelivery("sink", 1, "", "event", json.RawMessage(`{}`))
	r := Receipt{DeliveryVersion, "sink", 1, "event", d.EventHash, "receipt", d.EventHash, time.Now().UTC()}
	store, sink := &exportStore{claim: &ClaimedDelivery{d, "claim"}}, &exportSink{receipt: r}
	exporter, _ := NewExporter("sink", store, sink)
	didWork, err := exporter.ExportOne(t.Context(), time.Now().UTC())
	if err != nil || !didWork || sink.calls != 1 || store.completed != 1 || store.released != 0 {
		t.Fatalf("work=%v calls=%d complete=%d release=%d err=%v", didWork, sink.calls, store.completed, store.released, err)
	}
	sink.receipt.EventHash = "sha256:" + string(make([]byte, 64))
	if _, err := exporter.ExportOne(t.Context(), time.Now().UTC()); err == nil || store.released != 1 {
		t.Fatal("invalid receipt was not rejected and released")
	}
}

func TestDeliveryRejectsInvalidChainAndTampering(t *testing.T) {
	if _, err := NewDelivery("sink", 2, "", "event", json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing prior hash accepted")
	}
	d, _ := NewDelivery("sink", 1, "", "event", json.RawMessage(`{"a":1}`))
	d.Event = json.RawMessage(`{"a":2}`)
	if d.Validate() == nil {
		t.Fatal("tampered event accepted")
	}
}
