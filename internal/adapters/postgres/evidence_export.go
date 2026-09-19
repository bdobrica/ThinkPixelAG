package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
	"github.com/jackc/pgx/v5"
)

// EvidenceDeliveryStore serializes each sink's hash chain while permitting
// different independently administered sinks to progress concurrently.
type EvidenceDeliveryStore struct {
	db          DBTX
	lease       time.Duration
	mu          sync.Mutex
	pendingSink string
	pendingIDs  []string
}

func NewEvidenceDeliveryStore(db DBTX, lease time.Duration) (*EvidenceDeliveryStore, error) {
	if db == nil || lease <= 0 || lease > 10*time.Minute {
		return nil, errors.New("invalid evidence delivery store configuration")
	}
	return &EvidenceDeliveryStore{db: db, lease: lease}, nil
}

// Claim keeps only bounded candidate IDs in memory. PostgreSQL rechecks every
// candidate, receipt and lease; the hints never establish delivery authority.
func (s *EvidenceDeliveryStore) Claim(ctx context.Context, sinkID string, now time.Time) (*evidence.ClaimedDelivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pendingSink != sinkID {
		s.pendingSink, s.pendingIDs = sinkID, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		if len(s.pendingIDs) == 0 {
			if err := s.refillPending(ctx, sinkID, now); err != nil {
				return nil, err
			}
		}
		claim, err := s.claimCandidate(ctx, sinkID, now)
		if err != nil {
			return nil, err
		}
		if claim != nil {
			for i, id := range s.pendingIDs {
				if id == claim.Delivery.EventID {
					s.pendingIDs = append(s.pendingIDs[:i], s.pendingIDs[i+1:]...)
					break
				}
			}
			return claim, nil
		}
		// Another exporter may have consumed all hints. Refill once; do not
		// spin when its lease is still active or the sink is caught up.
		if len(s.pendingIDs) == 0 {
			return nil, nil
		}
		s.pendingIDs = nil
	}
	return nil, nil
}

func (s *EvidenceDeliveryStore) refillPending(ctx context.Context, sinkID string, now time.Time) error {
	if _, err := s.db.Exec(ctx, `INSERT INTO evidence_sink_checkpoints(sink_id,updated_at) VALUES($1,$2) ON CONFLICT(sink_id) DO NOTHING`, sinkID, now); err != nil {
		return fmt.Errorf("initialize evidence checkpoint: %w", err)
	}
	rows, err := s.db.Query(ctx, `SELECT o.id::text FROM outbox_messages o
WHERE NOT EXISTS (SELECT 1 FROM evidence_delivery_receipts r WHERE r.sink_id=$1 AND r.event_id=o.id)
ORDER BY o.occurred_at,o.id LIMIT 64`, sinkID)
	if err != nil {
		return fmt.Errorf("find pending evidence: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("decode pending evidence: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read pending evidence: %w", err)
	}
	s.pendingIDs = ids
	return nil
}

func (s *EvidenceDeliveryStore) claimCandidate(ctx context.Context, sinkID string, now time.Time) (*evidence.ClaimedDelivery, error) {
	token, err := domain.NewID()
	if err != nil {
		return nil, err
	}
	var eventID string
	var sequence int64
	var previous *string
	var payload []byte
	err = s.db.QueryRow(ctx, `WITH candidate AS MATERIALIZED (
 SELECT o.id,o.payload,c.last_sequence,c.claim_token,c.claimed_event_id,c.claimed_until
 FROM evidence_sink_checkpoints c
 JOIN LATERAL (
   SELECT c.claimed_event_id AS id WHERE c.claimed_event_id IS NOT NULL
   UNION ALL
   SELECT hint::uuid FROM unnest($5::text[]) hint WHERE c.claimed_event_id IS NULL
 ) choice ON true
 JOIN outbox_messages o ON o.id=choice.id
 WHERE c.sink_id=$1 AND (c.claimed_until IS NULL OR c.claimed_until <= $2::timestamptz)
 AND NOT EXISTS (SELECT 1 FROM evidence_delivery_receipts r WHERE r.sink_id=$1 AND r.event_id=o.id)
 ORDER BY o.occurred_at,o.id LIMIT 1
), claimed AS (
 UPDATE evidence_sink_checkpoints c SET claim_token=$3,claimed_event_id=x.id,claimed_until=$2+$4::interval,updated_at=$2
 FROM candidate x WHERE c.sink_id=$1
 AND c.last_sequence=x.last_sequence
 AND c.claim_token IS NOT DISTINCT FROM x.claim_token
 AND c.claimed_event_id IS NOT DISTINCT FROM x.claimed_event_id
 AND c.claimed_until IS NOT DISTINCT FROM x.claimed_until
 RETURNING x.id,c.last_sequence+1 AS sequence,c.last_event_hash,x.payload
) SELECT id,sequence,last_event_hash,payload FROM claimed`, sinkID, now, token.String(), s.lease.String(), s.pendingIDs).Scan(&eventID, &sequence, &previous, &payload)
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
	) , published AS (
 UPDATE outbox_messages SET published_at=$8 WHERE id=$2 AND published_at IS NULL
 AND EXISTS (SELECT 1 FROM inserted) RETURNING id
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

// Release expires ownership but retains the pending event: a lost HTTP response
// may follow remote acceptance, so a later event must not replace this sequence.
func (s *EvidenceDeliveryStore) Release(ctx context.Context, claim evidence.ClaimedDelivery) error {
	token, err := domain.NewID()
	if err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE evidence_sink_checkpoints SET claim_token=$4,claimed_until='-infinity'::timestamptz WHERE sink_id=$1 AND claim_token=$2 AND claimed_event_id=$3`, claim.Delivery.SinkID, claim.ClaimToken, claim.Delivery.EventID, token.String())
	if err != nil {
		return fmt.Errorf("release evidence delivery: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrOutboxClaimLost
	}
	return nil
}
