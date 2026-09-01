package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

const DeliveryVersion = "thinkpixelag.evidence-delivery/v1"

var deliveryDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Delivery struct {
	Version      string          `json:"version"`
	SinkID       string          `json:"sink_id"`
	Sequence     uint64          `json:"sequence"`
	EventID      string          `json:"event_id"`
	PreviousHash string          `json:"previous_hash,omitempty"`
	EventHash    string          `json:"event_hash"`
	Event        json.RawMessage `json:"event"`
}

type Receipt struct {
	Version    string    `json:"version"`
	SinkID     string    `json:"sink_id"`
	Sequence   uint64    `json:"sequence"`
	EventID    string    `json:"event_id"`
	EventHash  string    `json:"event_hash"`
	ReceiptID  string    `json:"receipt_id"`
	Checkpoint string    `json:"checkpoint"`
	AcceptedAt time.Time `json:"accepted_at"`
}

type Sink interface {
	Export(context.Context, Delivery) (Receipt, error)
}

type ClaimedDelivery struct {
	Delivery   Delivery
	ClaimToken string
}

// DeliveryStore owns durable replay position and receipt persistence. A claim
// must return the same sequence and predecessor after lease expiry until its
// receipt is committed.
type DeliveryStore interface {
	Claim(context.Context, string, time.Time) (*ClaimedDelivery, error)
	Complete(context.Context, ClaimedDelivery, Receipt) error
	Release(context.Context, ClaimedDelivery) error
}

type Exporter struct {
	sinkID string
	store  DeliveryStore
	sink   Sink
}

func NewExporter(sinkID string, store DeliveryStore, sink Sink) (*Exporter, error) {
	if !validReference(sinkID, 256) || store == nil || sink == nil {
		return nil, errors.New("evidence exporter requires sink identity, store, and sink")
	}
	return &Exporter{sinkID, store, sink}, nil
}

// ExportOne exports at most one ordered event. A nil claim means the durable
// source is caught up. Sink success followed by a process crash is safe: lease
// expiry replays the same event ID and hash link, and the sink deduplicates it.
func (e *Exporter) ExportOne(ctx context.Context, now time.Time) (bool, error) {
	if now.IsZero() || now.Location() != time.UTC {
		return false, errors.New("export time must be non-zero UTC")
	}
	claim, err := e.store.Claim(ctx, e.sinkID, now)
	if err != nil || claim == nil {
		return false, err
	}
	if claim.Delivery.SinkID != e.sinkID || !validReference(claim.ClaimToken, 128) {
		_ = e.store.Release(ctx, *claim)
		return false, errors.New("invalid evidence delivery claim")
	}
	receipt, err := e.sink.Export(ctx, claim.Delivery)
	if err != nil {
		return true, errors.Join(err, e.store.Release(ctx, *claim))
	}
	if err := receipt.ValidateFor(claim.Delivery); err != nil {
		_ = e.store.Release(ctx, *claim)
		return true, err
	}
	if err := e.store.Complete(ctx, *claim, receipt); err != nil {
		return true, err
	}
	return true, nil
}

// NewDelivery creates a stable hash link. Replaying the same event at an
// unchanged checkpoint produces byte-for-byte identical integrity metadata.
func NewDelivery(sinkID string, sequence uint64, previousHash, eventID string, event json.RawMessage) (Delivery, error) {
	if !validReference(sinkID, 256) || sequence == 0 || !validReference(eventID, 128) || (sequence == 1) != (previousHash == "") || previousHash != "" && !deliveryDigestPattern.MatchString(previousHash) || !validJSONObject(event) {
		return Delivery{}, errors.New("invalid evidence delivery binding")
	}
	canonical, err := json.Marshal(struct {
		Version, SinkID       string
		Sequence              uint64
		EventID, PreviousHash string
		Event                 json.RawMessage
	}{DeliveryVersion, sinkID, sequence, eventID, previousHash, event})
	if err != nil {
		return Delivery{}, fmt.Errorf("encode evidence delivery: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return Delivery{DeliveryVersion, sinkID, sequence, eventID, previousHash, "sha256:" + hex.EncodeToString(digest[:]), event}, nil
}

func (d Delivery) Validate() error {
	rebuilt, err := NewDelivery(d.SinkID, d.Sequence, d.PreviousHash, d.EventID, d.Event)
	if err != nil || rebuilt.Version != d.Version || rebuilt.EventHash != d.EventHash {
		return errors.New("invalid evidence delivery integrity metadata")
	}
	return nil
}

func (r Receipt) ValidateFor(d Delivery) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if r.Version != DeliveryVersion || r.SinkID != d.SinkID || r.Sequence != d.Sequence || r.EventID != d.EventID || r.EventHash != d.EventHash || !validReference(r.ReceiptID, 512) || !deliveryDigestPattern.MatchString(r.Checkpoint) || r.AcceptedAt.IsZero() || r.AcceptedAt.Location() != time.UTC {
		return errors.New("sink receipt does not bind the delivered evidence")
	}
	return nil
}

func validReference(value string, max int) bool { return len(value) > 0 && len(value) <= max }
func validJSONObject(value []byte) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(value, &object) == nil && object != nil
}
