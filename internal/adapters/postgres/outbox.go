package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

var ErrOutboxClaimLost = errors.New("outbox claim lost")

type ClaimedMessage struct {
	OutboxMessage
	ClaimToken domain.ID
	Attempt    int
}

type PublishError struct {
	Code      string
	Permanent bool
	Err       error
}

func (e *PublishError) Error() string { return fmt.Sprintf("publish outbox message: %s", e.Code) }
func (e *PublishError) Unwrap() error { return e.Err }

// EvidenceSink must deduplicate using message.ID. Delivery is at least once:
// the process can fail after Send succeeds but before PostgreSQL is updated.
type EvidenceSink interface {
	Send(context.Context, OutboxMessage) error
}

type OutboxConfig struct {
	WorkerID                   string
	BatchSize, MaxAttempts     int
	Lease, BaseRetry, MaxRetry time.Duration
	Jitter                     func(time.Duration) time.Duration
}

type OutboxPublisher struct {
	db     DBTX
	sink   EvidenceSink
	config OutboxConfig
}

func NewOutboxPublisher(db DBTX, sink EvidenceSink, config OutboxConfig) (*OutboxPublisher, error) {
	if db == nil || sink == nil {
		return nil, errors.New("outbox publisher requires database and sink")
	}
	if len(config.WorkerID) < 1 || len(config.WorkerID) > 256 || config.BatchSize < 1 || config.BatchSize > 1000 || config.MaxAttempts < 1 || config.Lease <= 0 || config.BaseRetry <= 0 || config.MaxRetry < config.BaseRetry {
		return nil, errors.New("invalid outbox publisher configuration")
	}
	if config.Jitter == nil {
		config.Jitter = func(bound time.Duration) time.Duration { return time.Duration(rand.Int64N(int64(bound) + 1)) }
	}
	return &OutboxPublisher{db: db, sink: sink, config: config}, nil
}

// PublishBatch claims a bounded batch using SKIP LOCKED and processes it. A
// returned count is the number claimed, including retry/dead-letter outcomes.
func (p *OutboxPublisher) PublishBatch(ctx context.Context, now time.Time) (int, error) {
	if _, err := domain.RequireUTC(now); err != nil {
		return 0, err
	}
	messages, err := p.claim(ctx, now)
	if err != nil {
		return 0, err
	}
	var failures []error
	for _, message := range messages {
		err := p.sink.Send(ctx, message.OutboxMessage)
		if err == nil {
			err = p.markPublished(ctx, message, now)
		} else {
			err = p.markFailed(ctx, message, now, err)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return len(messages), errors.Join(failures...)
}

func (p *OutboxPublisher) claim(ctx context.Context, now time.Time) ([]ClaimedMessage, error) {
	claimToken, err := domain.NewID()
	if err != nil {
		return nil, fmt.Errorf("create outbox claim token: %w", err)
	}
	rows, err := p.db.Query(ctx, `WITH candidates AS (
		SELECT id FROM outbox_messages
		WHERE published_at IS NULL AND dead_lettered_at IS NULL AND available_at <= $1
		  AND (claimed_until IS NULL OR claimed_until <= $1)
		ORDER BY available_at, occurred_at, id FOR UPDATE SKIP LOCKED LIMIT $2
	), claimed AS (
		UPDATE outbox_messages m SET claimed_by=$3, claim_token=$5, claimed_until=$1+$4::interval,
			attempt_count=m.attempt_count+1, last_attempt_at=$1
		FROM candidates c WHERE m.id=c.id
		RETURNING m.id,m.tenant_id,m.aggregate_type,m.aggregate_id,m.event_type,m.schema_version,m.payload,m.headers,m.occurred_at,m.available_at,m.claim_token,m.attempt_count
	) SELECT * FROM claimed ORDER BY available_at, occurred_at, id`, now, p.config.BatchSize, p.config.WorkerID, p.config.Lease.String(), claimToken.String())
	if err != nil {
		return nil, fmt.Errorf("claim outbox messages: %w", err)
	}
	defer rows.Close()
	var result []ClaimedMessage
	for rows.Next() {
		var id, tenant, token *string
		var m ClaimedMessage
		if err := rows.Scan(&id, &tenant, &m.AggregateType, &m.AggregateID, &m.EventType, &m.SchemaVersion, &m.Payload, &m.Headers, &m.OccurredAt, &m.AvailableAt, &token, &m.Attempt); err != nil {
			return nil, fmt.Errorf("scan outbox claim: %w", err)
		}
		if id == nil || token == nil {
			return nil, errors.New("invalid outbox claim")
		}
		parsed, err := domain.ParseID(*id)
		if err != nil {
			return nil, err
		}
		m.ID = parsed
		parsed, err = domain.ParseID(*token)
		if err != nil {
			return nil, err
		}
		m.ClaimToken = parsed
		if tenant != nil {
			parsed, err = domain.ParseID(*tenant)
			if err != nil {
				return nil, err
			}
			m.TenantID = &parsed
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox claims: %w", err)
	}
	return result, nil
}

func (p *OutboxPublisher) markPublished(ctx context.Context, m ClaimedMessage, now time.Time) error {
	tag, err := p.db.Exec(ctx, `UPDATE outbox_messages SET published_at=$1,claimed_by=NULL,claim_token=NULL,claimed_until=NULL,last_error_code=NULL WHERE id=$2 AND claim_token=$3 AND published_at IS NULL AND dead_lettered_at IS NULL`, now, m.ID.String(), m.ClaimToken.String())
	if err != nil {
		return fmt.Errorf("mark outbox published: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrOutboxClaimLost
	}
	return nil
}

func (p *OutboxPublisher) markFailed(ctx context.Context, m ClaimedMessage, now time.Time, sendErr error) error {
	code, permanent := "sink_error", false
	var classified *PublishError
	if errors.As(sendErr, &classified) {
		code, permanent = classified.Code, classified.Permanent
	}
	if len(code) < 1 || len(code) > 128 {
		code = "invalid_error_code"
		permanent = true
	}
	dead := permanent || m.Attempt >= p.config.MaxAttempts
	delay := p.retryDelay(m.Attempt)
	tag, err := p.db.Exec(ctx, `UPDATE outbox_messages SET claimed_by=NULL,claim_token=NULL,claimed_until=NULL,last_error_code=$1,
		available_at=CASE WHEN $2::boolean THEN available_at ELSE $3::timestamptz END,
		dead_lettered_at=CASE WHEN $2::boolean THEN $4::timestamptz ELSE NULL::timestamptz END
		WHERE id=$5 AND claim_token=$6 AND published_at IS NULL AND dead_lettered_at IS NULL`, code, dead, now.Add(delay), now, m.ID.String(), m.ClaimToken.String())
	if err != nil {
		return fmt.Errorf("record outbox failure: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrOutboxClaimLost
	}
	return nil
}

func (p *OutboxPublisher) retryDelay(attempt int) time.Duration {
	exponent := min(attempt-1, 62)
	base := float64(p.config.BaseRetry) * math.Pow(2, float64(exponent))
	if base > float64(p.config.MaxRetry) {
		base = float64(p.config.MaxRetry)
	}
	bounded := time.Duration(base)
	jitterBound := bounded / 2
	jitter := p.config.Jitter(jitterBound)
	if jitter < 0 {
		jitter = 0
	}
	if jitter > jitterBound {
		jitter = jitterBound
	}
	return jitterBound + jitter
}
