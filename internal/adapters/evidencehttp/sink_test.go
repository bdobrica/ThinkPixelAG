package evidencehttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
)

func TestAuthenticatedExportAndReceipt(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer sink-secret" || r.Header.Get("Idempotency-Key") != "event-1" {
			t.Error("authentication or replay key missing")
		}
		var d evidence.Delivery
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			t.Error(err)
			return
		}
		_ = json.NewEncoder(w).Encode(evidence.Receipt{Version: evidence.DeliveryVersion, SinkID: d.SinkID, Sequence: d.Sequence, EventID: d.EventID, EventHash: d.EventHash, ReceiptID: "receipt-1", Checkpoint: d.EventHash, AcceptedAt: time.Now().UTC()})
	}))
	defer server.Close()
	sink, err := New(Config{server.URL, "sink-secret", time.Second, 4096}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	d, _ := evidence.NewDelivery("sink", 1, "", "event-1", json.RawMessage(`{"type":"POLICY"}`))
	if _, err = sink.Export(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	if _, err = sink.Export(t.Context(), d); err != nil || calls != 2 {
		t.Fatalf("replay calls=%d err=%v", calls, err)
	}
}

func TestSinkFailsClosed(t *testing.T) {
	if _, err := New(Config{"http://sink.example/evidence", "token", time.Second, 100}, nil); err == nil {
		t.Fatal("plaintext endpoint accepted")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 101))) }))
	defer server.Close()
	sink, _ := New(Config{server.URL, "token", time.Second, 100}, server.Client())
	d, _ := evidence.NewDelivery("sink", 1, "", "event", json.RawMessage(`{}`))
	if _, err := sink.Export(t.Context(), d); err == nil {
		t.Fatal("oversized receipt accepted")
	}
}
