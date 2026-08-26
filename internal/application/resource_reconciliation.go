package application

import (
	"context"
	"errors"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const maxResourceReconciliationBatch = 1000

type ResourceReconciliationService struct {
	repository ports.ResourceReconciliationRepository
	clock      domain.Clock
}

func NewResourceReconciliationService(repository ports.ResourceReconciliationRepository, clock domain.Clock) (*ResourceReconciliationService, error) {
	if repository == nil || clock == nil {
		return nil, errors.New("resource reconciliation requires a repository and clock")
	}
	return &ResourceReconciliationService{repository: repository, clock: clock}, nil
}

// Reconcile processes at most limit safe candidates. Each candidate commits in
// its own transaction, bounding lock duration and making a crash between items
// equivalent to a normal retry.
func (s *ResourceReconciliationService) Reconcile(ctx context.Context, tenantID, systemPrincipalID domain.ID, limit int) ([]domain.ResourceReconciliationResult, error) {
	if tenantID.IsZero() || systemPrincipalID.IsZero() || limit <= 0 || limit > maxResourceReconciliationBatch {
		return nil, domain.NewError(domain.CodeInvalidArgument, "resource reconciliation request is invalid")
	}
	now, err := domain.RequireUTC(s.clock.Now())
	if err != nil || now.IsZero() {
		return nil, domain.NewError(domain.CodeInternal, "resource reconciliation clock is invalid")
	}
	results := make([]domain.ResourceReconciliationResult, 0, limit)
	for len(results) < limit {
		ids := make([]domain.ID, 5)
		for i := range ids {
			ids[i], err = domain.NewID()
			if err != nil {
				return results, domain.WrapError(domain.CodeInternal, "could not generate reconciliation evidence", err)
			}
		}
		result, reconcileErr := s.repository.ReconcileNextReservation(ctx, tenantID, systemPrincipalID, now, ports.ResourceReconciliationEvidence{
			SettlementID: ids[0], PolicyDecisionID: ids[1], AuditID: ids[2], OutboxID: ids[3], RunEventID: ids[4],
		})
		if errors.Is(reconcileErr, domain.ErrResourceReconciliationUnavailable) {
			return results, nil
		}
		if reconcileErr != nil {
			return results, reconcileErr
		}
		results = append(results, result)
	}
	return results, nil
}
