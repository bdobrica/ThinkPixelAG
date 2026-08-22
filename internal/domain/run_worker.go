package domain

import (
	"errors"
	"math"
	"time"
)

var (
	ErrRunLeaseUnavailable = errors.New("run lease is unavailable")
	ErrRunLeaseStale       = errors.New("run lease is stale")
)

// RunLease is an opaque worker claim. Both LeaseID and FencingToken must be
// presented on every subsequent operation; the token increases on every claim.
type RunLease struct {
	RunID, TenantID, LeaseID, WorkerID ID
	FencingToken                       int64
	ExpiresAt                          time.Time
}

func (lease RunLease) Validate() error {
	if lease.RunID.IsZero() || lease.TenantID.IsZero() || lease.LeaseID.IsZero() || lease.WorkerID.IsZero() || lease.FencingToken <= 0 {
		return errors.New("run lease identity is invalid")
	}
	expires, err := RequireUTC(lease.ExpiresAt)
	if err != nil || expires.IsZero() {
		return errors.New("run lease expiry must be non-zero UTC")
	}
	return nil
}

func (lease RunLease) ActiveAt(now time.Time) bool {
	now, err := RequireUTC(now)
	return err == nil && lease.Validate() == nil && now.Before(lease.ExpiresAt)
}

func NextFencingToken(current int64) (int64, error) {
	if current < 0 || current == math.MaxInt64 {
		return 0, ErrRunVersionConflict
	}
	return current + 1, nil
}

type WorkerRunOperation string

const (
	WorkerRunStart    WorkerRunOperation = "START"
	WorkerRunComplete WorkerRunOperation = "COMPLETE"
	WorkerRunFail     WorkerRunOperation = "FAIL"
	WorkerRunTimeout  WorkerRunOperation = "TIMEOUT"
)

func (operation WorkerRunOperation) Target() (RunState, RunActor, error) {
	switch operation {
	case WorkerRunStart:
		return RunRunning, RunActorWorker, nil
	case WorkerRunComplete:
		return RunCompleted, RunActorWorker, nil
	case WorkerRunFail:
		return RunFailed, RunActorWorker, nil
	case WorkerRunTimeout:
		return RunTimedOut, RunActorSystem, nil
	default:
		return "", "", errors.New("invalid worker run operation")
	}
}
