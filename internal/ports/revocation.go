package ports

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type RevocationEvidence struct {
	ChangeID, EventID, AuditID, OutboxID, RequestID, PolicyDecisionID domain.ID
	ReasonCodes                                                       []string
}
type RevocationRepository interface {
	CreateRevocation(context.Context, domain.Revocation, RevocationEvidence) (domain.RevocationResult, error)
	LiftRevocation(context.Context, domain.RevocationLift, RevocationEvidence) (domain.RevocationResult, error)
}
