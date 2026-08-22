package domain

import (
	"errors"
	"time"
)

// RunCancellation is an authorized, idempotent request to establish the
// CANCELLED terminal state. The actor is always derived from verified caller
// identity at the application boundary.
type RunCancellation struct {
	TenantID, RunID, ActorPrincipalID ID
	IdempotencyKey                    string
	ReasonCode                        string
	ExpectedStateVersion              *int64
	CreatedAt                         time.Time
}

func (cancellation RunCancellation) Validate() error {
	if cancellation.TenantID.IsZero() || cancellation.RunID.IsZero() || cancellation.ActorPrincipalID.IsZero() || len(cancellation.IdempotencyKey) < 1 || len(cancellation.IdempotencyKey) > 256 {
		return errors.New("run cancellation identity is invalid")
	}
	if cancellation.ReasonCode != "" && !signalNamePattern.MatchString(cancellation.ReasonCode) {
		return errors.New("run cancellation reason code is invalid")
	}
	if cancellation.ExpectedStateVersion != nil && *cancellation.ExpectedStateVersion < 1 {
		return ErrRunVersionConflict
	}
	if _, err := RequireUTC(cancellation.CreatedAt); err != nil || cancellation.CreatedAt.IsZero() {
		return errors.New("run cancellation time must be non-zero UTC")
	}
	return nil
}
