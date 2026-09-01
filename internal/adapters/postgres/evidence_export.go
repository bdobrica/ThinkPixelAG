package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
	"github.com/jackc/pgx/v5"
)

// EvidenceDeliveryStore serializes each sink's hash chain while permitting
// different independently administered sinks to progress concurrently.
type EvidenceDeliveryStore struct {
	db    DBTX
	lease time.Duration
}

func NewEvidenceDeliveryStore(db DBTX, lease time.Duration) (*EvidenceDeliveryStore, error) {
	if db == nil || lease <= 0 || lease > 10*time.Minute {
		return nil, errors.New("invalid evidence delivery store configuration")
	}
	return &EvidenceDeliveryStore{db, lease}, nil
}

func (s *EvidenceDeliveryStore) Claim(ctx context.Context, sinkID string, now time.Time) (*evidence.ClaimedDelivery, error) {
	token, err := domain.NewID()
	if err != nil {
		return nil, err
	}
	if _, err = s.db.Exec(ctx, `INSERT INTO evidence_sink_checkpoints(sink_id,updated_at) VALUES($1,$2) ON CONFLICT(sink_id) DO NOTHING`, sinkID, now); err != nil {
		return nil, fmt.Errorf("initialize evidence checkpoint: %w", err)
	}
	var eventID string
	var sequence int64
	var previous *string
	var payload []byte
	err = s.db.QueryRow(ctx, `WITH candidate AS (
	 SELECT o.id,o.payload FROM outbox_messages o
	 WHERE NOT EXISTS (SELECT 1 FROM evidence_delivery_receipts r WHERE r.sink_id=$1::text AND r.event_id=o.id)
	 ORDER BY o.occurred_at,o.id LIMIT 1
	), claimed AS (
	 UPDATE evidence_sink_checkpoints c SET claim_token=$3,claimed_event_id=x.id,claimed_until=$2+$4::interval,updated_at=$2
	 FROM candidate x WHERE c.sink_id=$1::text AND (c.claimed_until IS NULL OR c.claimed_until <= $2::timestamptz)
	 RETURNING x.id,c.last_sequence+1 AS sequence,c.last_event_hash,x.payload
	) SELECT id,sequence,last_event_hash,payload FROM claimed`, sinkID, now, token.String(), s.lease.String()).Scan(&eventID, &sequence, &previous, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim evidence delivery: %w", err)
	}
	prior := ""
	if previous != nil {
		prior = *previous
	}
	delivery, err := evidence.NewDelivery(sinkID, uint64(sequence), prior, eventID, json.RawMessage(payload))
	if err != nil {
		return nil, fmt.Errorf("build evidence delivery: %w", err)
	}
	return &evidence.ClaimedDelivery{Delivery: delivery, ClaimToken: token.String()}, nil
}

func (s *EvidenceDeliveryStore) Complete(ctx context.Context, claim evidence.ClaimedDelivery, receipt evidence.Receipt) error {
	if err := receipt.ValidateFor(claim.Delivery); err != nil {
		return err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `WITH inserted AS (
	 INSERT INTO evidence_delivery_receipts(sink_id,event_id,sequence,previous_event_hash,event_hash,receipt_id,sink_checkpoint,accepted_at,receipt)
	 SELECT $1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9::jsonb FROM evidence_sink_checkpoints c
	 WHERE c.sink_id=$1 AND c.claim_token=$10 AND c.claimed_event_id=$2 AND c.last_sequence+1=$3
	 RETURNING sink_id
	) UPDATE evidence_sink_checkpoints c SET last_sequence=$3,last_event_hash=$5,claim_token=NULL,claimed_event_id=NULL,claimed_until=NULL,updated_at=$8
	 FROM inserted i WHERE c.sink_id=i.sink_id`, claim.Delivery.SinkID, claim.Delivery.EventID, claim.Delivery.Sequence, claim.Delivery.PreviousHash, claim.Delivery.EventHash, receipt.ReceiptID, receipt.Checkpoint, receipt.AcceptedAt, raw, claim.ClaimToken)
	if err != nil {
		return fmt.Errorf("commit evidence receipt: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrOutboxClaimLost
	}
	return nil
}

func (s *EvidenceDeliveryStore) Release(ctx context.Context, claim evidence.ClaimedDelivery) error {
	tag, err := s.db.Exec(ctx, `UPDATE evidence_sink_checkpoints SET claim_token=NULL,claimed_event_id=NULL,claimed_until=NULL WHERE sink_id=$1 AND claim_token=$2 AND claimed_event_id=$3`, claim.Delivery.SinkID, claim.ClaimToken, claim.Delivery.EventID)
	if err != nil {
		return fmt.Errorf("release evidence delivery: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrOutboxClaimLost
	}
	return nil
}
