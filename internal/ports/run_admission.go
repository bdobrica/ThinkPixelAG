package ports

import (
	"context"
	"time"

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

// RunAdmissionResponseEncoder constructs the bounded, sanitized replay response
// before any authoritative admission mutation is committed.
type RunAdmissionResponseEncoder func(domain.RunAdmission) (IdempotencyResponse, error)

// IdempotentRunAdmissionRepository commits the admission and the acquired
// request's replay result in the same transaction. Lost ownership or a failed
// completion must leave neither the Run nor its evidence committed.
type IdempotentRunAdmissionRepository interface {
	AdmitRunIdempotently(context.Context, domain.RunAdmission, domain.RunVersionResolution, RunAdmissionEvidence, IdempotencyAcquisition, IdempotencyResponse, time.Time) error
}
