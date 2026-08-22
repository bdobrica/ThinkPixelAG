package domain

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestRunLeaseValidationAndActivity(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	lease := RunLease{RunID: mustID(t), TenantID: mustID(t), LeaseID: mustID(t), WorkerID: mustID(t), FencingToken: 1, ExpiresAt: now.Add(time.Minute)}
	if err := lease.Validate(); err != nil || !lease.ActiveAt(now) || lease.ActiveAt(lease.ExpiresAt) {
		t.Fatalf("lease validation/activity = %v, %v, %v", err, lease.ActiveAt(now), lease.ActiveAt(lease.ExpiresAt))
	}
	lease.FencingToken = 0
	if lease.Validate() == nil {
		t.Fatal("zero fence accepted")
	}
}

func TestWorkerRunOperationTargets(t *testing.T) {
	tests := []struct {
		operation WorkerRunOperation
		state     RunState
		actor     RunActor
	}{
		{WorkerRunStart, RunRunning, RunActorWorker}, {WorkerRunComplete, RunCompleted, RunActorWorker},
		{WorkerRunFail, RunFailed, RunActorWorker}, {WorkerRunTimeout, RunTimedOut, RunActorSystem},
	}
	for _, test := range tests {
		state, actor, err := test.operation.Target()
		if err != nil || state != test.state || actor != test.actor {
			t.Fatalf("%s = %s/%s/%v", test.operation, state, actor, err)
		}
	}
	if _, _, err := WorkerRunOperation("DELETE").Target(); err == nil {
		t.Fatal("invalid operation accepted")
	}
	if _, err := NextFencingToken(math.MaxInt64); !errors.Is(err, ErrRunVersionConflict) {
		t.Fatalf("overflow = %v", err)
	}
}
