package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type RunCancellationEvidence struct {
	SignalID, EventID, AuditID, OutboxID, RequestID, PolicyDecisionID domain.ID
	ReasonCodes                                                       []string
}

type RunCancellationRepository interface {
	GetRun(context.Context, domain.ID) (RunAccessRecord, error)
	CancelRun(context.Context, domain.RunCancellation, RunCancellationEvidence) (domain.Run, error)
}
