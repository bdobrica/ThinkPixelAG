package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type RunAdmissionEvidence struct {
	EventID, AuditID, OutboxID, RequestID domain.ID
	ReasonCodes                           []string
}

// RunAdmissionRepository commits the complete admission aggregate and its
// evidence atomically.
type RunAdmissionRepository interface {
	AdmitRun(context.Context, domain.RunAdmission, domain.RunVersionResolution, RunAdmissionEvidence) error
}
