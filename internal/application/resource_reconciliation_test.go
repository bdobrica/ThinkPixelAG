package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type reconciliationRepositoryStub struct {
	calls int
	now   time.Time
}

func (s *reconciliationRepositoryStub) ReconcileNextReservation(_ context.Context, tenant, actor domain.ID, now time.Time, evidence ports.ResourceReconciliationEvidence) (domain.ResourceReconciliationResult, error) {
	s.calls++
	s.now = now
	if tenant.IsZero() || actor.IsZero() || evidence.SettlementID.IsZero() || evidence.RunEventID.IsZero() {
		return domain.ResourceReconciliationResult{}, errors.New("invalid call")
	}
	if s.calls > 2 {
		return domain.ResourceReconciliationResult{}, domain.ErrResourceReconciliationUnavailable
	}
	return domain.ResourceReconciliationResult{Expired: s.calls == 2}, nil
}

func TestResourceReconciliationProcessesBoundedReplaySafeItems(t *testing.T) {
	now := time.Unix(50, 0).UTC()
	repository := &reconciliationRepositoryStub{}
	service, err := NewResourceReconciliationService(repository, fixedClock{now: now})
	if err != nil {
		t.Fatal(err)
	}
	tenant, actor := applicationID(t), applicationID(t)
	results, err := service.Reconcile(context.Background(), tenant, actor, 10)
	if err != nil || len(results) != 2 || !results[1].Expired || repository.calls != 3 || repository.now != now {
		t.Fatalf("results=%+v calls=%d now=%v error=%v", results, repository.calls, repository.now, err)
	}
	if _, err := service.Reconcile(context.Background(), tenant, actor, 0); domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("invalid limit error=%v", err)
	}
}
