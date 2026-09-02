package ports

import (
	"context"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type RuntimeActivePolicy struct {
	TenantID, Channel, BundleID, Digest string
	Version                             int64
	ActivatedAt, RefreshedAt            time.Time
}

// RuntimeSecurityState is the authoritative policy and revocation state a
// serving process must load before it can report ready.
type RuntimeSecurityState struct {
	Policy   RuntimeActivePolicy
	Sequence int64
	Epochs   domain.EpochVector
}

// RuntimeSecurityStateSource keeps production readiness independent from the
// persistence implementation used to reconcile security state.
type RuntimeSecurityStateSource interface {
	LoadRuntimeSecurityState(context.Context, time.Time) ([]RuntimeSecurityState, error)
}
