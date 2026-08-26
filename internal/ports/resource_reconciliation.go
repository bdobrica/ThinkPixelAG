package ports

import (
	"context"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type ResourceReconciliationEvidence struct {
	SettlementID, PolicyDecisionID, AuditID, OutboxID, RunEventID domain.ID
}

type ResourceReconciliationRepository interface {
	ReconcileNextReservation(context.Context, domain.ID, domain.ID, time.Time, ResourceReconciliationEvidence) (domain.ResourceReconciliationResult, error)
}
