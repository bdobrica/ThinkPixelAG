package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type RunSignalEvidence struct {
	EventID, AuditID, OutboxID, RequestID, PolicyDecisionID domain.ID
	ReasonCodes                                             []string
}

type RunSignalRepository interface {
	GetRun(context.Context, domain.ID) (RunAccessRecord, error)
	AppendRunSignal(context.Context, domain.RunSignal, RunSignalEvidence) (domain.RunEvent, error)
}
