package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

// IdempotencyStore safely binds every operation to the authenticated tenant.
type IdempotencyStore struct{ repositories *Repositories }

func NewIdempotencyStore(repositories *Repositories) (*IdempotencyStore, error) {
	if repositories == nil {
		return nil, errors.New("idempotency store requires repositories")
	}
	return &IdempotencyStore{repositories: repositories}, nil
}

func (s *IdempotencyStore) tenant(tenantID domain.ID) (*TenantRepository, error) {
	return s.repositories.ForTenant(tenantID)
}

func (s *IdempotencyStore) AcquireIdempotency(ctx context.Context, tenantID domain.ID, request ports.IdempotencyRequest, now time.Time) (ports.IdempotencyAcquisition, error) {
	repository, err := s.tenant(tenantID)
	if err != nil {
		return ports.IdempotencyAcquisition{}, err
	}
	result, err := repository.AcquireIdempotency(ctx, request, now)
	if errors.Is(err, ErrIdempotencyConflict) {
		return ports.IdempotencyAcquisition{}, domain.NewError(domain.CodeConflict, "idempotency key was already used for a different request")
	}
	if errors.Is(err, ErrIdempotencyInFlight) {
		return ports.IdempotencyAcquisition{}, domain.NewError(domain.CodeConflict, "idempotent request is already in progress").WithRetryable()
	}
	return result, err
}

func (s *IdempotencyStore) CompleteIdempotency(ctx context.Context, tenantID domain.ID, acquisition ports.IdempotencyAcquisition, response ports.IdempotencyResponse, now time.Time) error {
	repository, err := s.tenant(tenantID)
	if err != nil {
		return err
	}
	return repository.CompleteIdempotency(ctx, acquisition, response, now)
}

func (s *IdempotencyStore) FailIdempotency(ctx context.Context, tenantID domain.ID, acquisition ports.IdempotencyAcquisition, now time.Time) error {
	repository, err := s.tenant(tenantID)
	if err != nil {
		return err
	}
	return repository.FailIdempotency(ctx, acquisition, now)
}
