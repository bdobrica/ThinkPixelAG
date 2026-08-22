package application

import (
	"context"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type workerRepositoryStub struct {
	claimed, heartbeat domain.RunLease
	operation          domain.WorkerRunOperation
}

func (stub *workerRepositoryStub) ClaimRun(_ context.Context, tenant, worker, lease domain.ID, _, expires time.Time) (domain.RunLease, error) {
	stub.claimed = domain.RunLease{RunID: lease, TenantID: tenant, WorkerID: worker, LeaseID: lease, FencingToken: 1, ExpiresAt: expires}
	return stub.claimed, nil
}
func (stub *workerRepositoryStub) HeartbeatRun(_ context.Context, lease domain.RunLease, _, expires time.Time) (domain.RunLease, error) {
	lease.ExpiresAt = expires
	stub.heartbeat = lease
	return lease, nil
}
func (stub *workerRepositoryStub) MutateWorkerRun(_ context.Context, lease domain.RunLease, operation domain.WorkerRunOperation, _ domain.ID, _ time.Time) (domain.Run, error) {
	stub.operation = operation
	return domain.Run{ID: lease.RunID}, nil
}

func TestRunWorkerClaimHeartbeatAndOperate(t *testing.T) {
	now := testTime()
	repository := &workerRepositoryStub{}
	service, err := NewRunWorkerService(repository, fixedClock{now: now}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	tenant, worker := applicationID(t), applicationID(t)
	lease, err := service.Claim(context.Background(), tenant, worker)
	if err != nil || lease.TenantID != tenant || lease.WorkerID != worker || lease.ExpiresAt != now.Add(time.Minute) {
		t.Fatalf("claim=%+v error=%v", lease, err)
	}
	service.clock = fixedClock{now: now.Add(30 * time.Second)}
	lease, err = service.Heartbeat(context.Background(), lease)
	if err != nil || lease.ExpiresAt != now.Add(90*time.Second) {
		t.Fatalf("heartbeat=%+v error=%v", lease, err)
	}
	if _, err = service.Operate(context.Background(), lease, domain.WorkerRunStart); err != nil || repository.operation != domain.WorkerRunStart {
		t.Fatalf("operation=%s error=%v", repository.operation, err)
	}
}

func TestRunWorkerRejectsExpiredLeaseAndInvalidConfiguration(t *testing.T) {
	now := testTime()
	repository := &workerRepositoryStub{}
	if _, err := NewRunWorkerService(repository, fixedClock{now: now}, 6*time.Minute); err == nil {
		t.Fatal("unbounded lease accepted")
	}
	service, _ := NewRunWorkerService(repository, fixedClock{now: now}, time.Minute)
	lease := domain.RunLease{RunID: applicationID(t), TenantID: applicationID(t), LeaseID: applicationID(t), WorkerID: applicationID(t), FencingToken: 1, ExpiresAt: now}
	if _, err := service.Heartbeat(context.Background(), lease); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("heartbeat error=%v", err)
	}
	if _, err := service.Operate(context.Background(), lease, domain.WorkerRunComplete); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("operation error=%v", err)
	}
}
