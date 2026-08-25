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

// ChildRunAdmissionRepository commits an authorized child aggregate and its
// resource delegation as one indivisible admission.
type ChildRunAdmissionRepository interface {
	AdmitChildRun(context.Context, domain.RunAdmission, domain.RunVersionResolution, RunAdmissionEvidence, domain.ResourceReservation) (domain.ResourceReservation, error)
}
