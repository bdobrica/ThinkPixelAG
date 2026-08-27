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
	revocations   ports.RevocationAuthority
}

func NewRunWorkerService(repository ports.RunWorkerRepository, clock domain.Clock, leaseDuration time.Duration) (*RunWorkerService, error) {
	if repository == nil || clock == nil || leaseDuration <= 0 || leaseDuration > maxRunLease {
		return nil, errors.New("run worker service requires a repository, clock, and bounded lease duration")
	}
	return &RunWorkerService{repository: repository, clock: clock, leaseDuration: leaseDuration}, nil
}

func NewGovernedRunWorkerService(repository ports.RunWorkerRepository, revocations ports.RevocationAuthority, clock domain.Clock, leaseDuration time.Duration) (*RunWorkerService, error) {
	s, err := NewRunWorkerService(repository, clock, leaseDuration)
	if err != nil {
		return nil, err
	}
	if revocations == nil {
		return nil, errors.New("governed run worker requires revocation authority")
	}
	s.revocations = revocations
	return s, nil
}

func (service *RunWorkerService) Claim(ctx context.Context, tenantID, workerID domain.ID) (domain.RunLease, error) {
	if tenantID.IsZero() || workerID.IsZero() {
		return domain.RunLease{}, domain.NewError(domain.CodeInvalidArgument, "worker claim is invalid")
	}
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.RunLease{}, domain.WrapError(domain.CodeInternal, "worker clock is invalid", err)
	}
	if err = service.checkWorkerRevocations(ctx, tenantID, workerID, domain.ID{}, now); err != nil {
		return domain.RunLease{}, err
	}
	leaseID, err := domain.NewID()
	if err != nil {
		return domain.RunLease{}, domain.WrapError(domain.CodeInternal, "could not generate lease identifier", err)
	}
	lease, err := service.repository.ClaimRun(ctx, tenantID, workerID, leaseID, now, now.Add(service.leaseDuration))
	if err == nil {
		err = service.checkWorkerRevocations(ctx, tenantID, workerID, lease.RunID, now)
	}
	return lease, err
}

func (service *RunWorkerService) Heartbeat(ctx context.Context, lease domain.RunLease) (domain.RunLease, error) {
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.RunLease{}, domain.WrapError(domain.CodeInternal, "worker clock is invalid", err)
	}
	if !lease.ActiveAt(now) {
		return domain.RunLease{}, domain.NewError(domain.CodeConflict, "run lease is stale")
	}
	if err = service.checkWorkerRevocations(ctx, lease.TenantID, lease.WorkerID, lease.RunID, now); err != nil {
		return domain.RunLease{}, err
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
	if err = service.checkWorkerRevocations(ctx, lease.TenantID, lease.WorkerID, lease.RunID, now); err != nil {
		return domain.Run{}, err
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

func (service *RunWorkerService) checkWorkerRevocations(ctx context.Context, tenant, worker, run domain.ID, now time.Time) error {
	if service.revocations == nil {
		return nil
	}
	state, err := service.revocations.AuthoritativeRevocations(ctx, tenant, domain.ID{}, now)
	if err != nil {
		return domain.WrapError(domain.CodeUnavailable, "worker revocation authority is unavailable", err).WithRetryable()
	}
	for _, v := range state.Active {
		if v.Scope == domain.RevocationGlobal || (v.Scope == domain.RevocationTenantID && v.Target == tenant.String()) || (v.Scope == domain.RevocationPrincipalID && v.Target == worker.String()) || (!run.IsZero() && v.Scope == domain.RevocationRunID && v.Target == run.String()) {
			return domain.NewError(domain.CodeForbidden, "worker operation is revoked")
		}
	}
	return nil
}
