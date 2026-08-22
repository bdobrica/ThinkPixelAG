package application

import (
	"context"
	"errors"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const maxRunLease = 5 * time.Minute

type RunWorkerService struct {
	repository    ports.RunWorkerRepository
	clock         domain.Clock
	leaseDuration time.Duration
}

func NewRunWorkerService(repository ports.RunWorkerRepository, clock domain.Clock, leaseDuration time.Duration) (*RunWorkerService, error) {
	if repository == nil || clock == nil || leaseDuration <= 0 || leaseDuration > maxRunLease {
		return nil, errors.New("run worker service requires a repository, clock, and bounded lease duration")
	}
	return &RunWorkerService{repository: repository, clock: clock, leaseDuration: leaseDuration}, nil
}

func (service *RunWorkerService) Claim(ctx context.Context, tenantID, workerID domain.ID) (domain.RunLease, error) {
	if tenantID.IsZero() || workerID.IsZero() {
		return domain.RunLease{}, domain.NewError(domain.CodeInvalidArgument, "worker claim is invalid")
	}
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.RunLease{}, domain.WrapError(domain.CodeInternal, "worker clock is invalid", err)
	}
	leaseID, err := domain.NewID()
	if err != nil {
		return domain.RunLease{}, domain.WrapError(domain.CodeInternal, "could not generate lease identifier", err)
	}
	return service.repository.ClaimRun(ctx, tenantID, workerID, leaseID, now, now.Add(service.leaseDuration))
}

func (service *RunWorkerService) Heartbeat(ctx context.Context, lease domain.RunLease) (domain.RunLease, error) {
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.RunLease{}, domain.WrapError(domain.CodeInternal, "worker clock is invalid", err)
	}
	if !lease.ActiveAt(now) {
		return domain.RunLease{}, domain.NewError(domain.CodeConflict, "run lease is stale")
	}
	return service.repository.HeartbeatRun(ctx, lease, now, now.Add(service.leaseDuration))
}

func (service *RunWorkerService) Operate(ctx context.Context, lease domain.RunLease, operation domain.WorkerRunOperation) (domain.Run, error) {
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.Run{}, domain.WrapError(domain.CodeInternal, "worker clock is invalid", err)
	}
	if !lease.ActiveAt(now) {
		return domain.Run{}, domain.NewError(domain.CodeConflict, "run lease is stale")
	}
	if _, _, err := operation.Target(); err != nil {
		return domain.Run{}, domain.NewError(domain.CodeInvalidArgument, "worker operation is invalid")
	}
	eventID, err := domain.NewID()
	if err != nil {
		return domain.Run{}, domain.WrapError(domain.CodeInternal, "could not generate worker event identifier", err)
	}
	return service.repository.MutateWorkerRun(ctx, lease, operation, eventID, now)
}
