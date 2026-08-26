package ports

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type ResourceSettlementEvidence struct {
	AuditID, OutboxID, RequestID domain.ID
	ReasonCodes                  []string
}

type ResourceSettlementTarget struct {
	Run        RunAccessRecord
	ChildRunID domain.ID
}

type ResourceSettlementRepository interface {
	GetSettlementTarget(context.Context, domain.ID) (ResourceSettlementTarget, error)
	SettleReservation(context.Context, domain.ResourceSettlement, ResourceSettlementEvidence) (domain.ResourceSettlementResult, error)
}
