package ports

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type IdempotencyOutcome string

const (
	IdempotencyAcquired IdempotencyOutcome = "acquired"
	IdempotencyReplay   IdempotencyOutcome = "replay"
)

type IdempotencyRequest struct {
	PrincipalID             domain.ID
	Route, Key, RequestHash string
	Lease, TTL              time.Duration
}

type IdempotencyResponse struct {
	Status  int
	Headers json.RawMessage
	Body    []byte
}

type IdempotencyAcquisition struct {
	Outcome              IdempotencyOutcome
	RecordID, OwnerToken domain.ID
	Response             *IdempotencyResponse
}

// IdempotencyStore is tenant-explicit so transport code cannot accidentally
// select authority from a request body or header.
type IdempotencyStore interface {
	AcquireIdempotency(context.Context, domain.ID, IdempotencyRequest, time.Time) (IdempotencyAcquisition, error)
	CompleteIdempotency(context.Context, domain.ID, IdempotencyAcquisition, IdempotencyResponse, time.Time) error
	FailIdempotency(context.Context, domain.ID, IdempotencyAcquisition, time.Time) error
}
