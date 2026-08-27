package ports

import (
	"context"
	"errors"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"time"
)

type RevocationEvidence struct {
	ChangeID, EventID, AuditID, OutboxID, RequestID, PolicyDecisionID domain.ID
	ReasonCodes                                                       []string
}
type RevocationRepository interface {
	CreateRevocation(context.Context, domain.Revocation, RevocationEvidence) (domain.RevocationResult, error)
	LiftRevocation(context.Context, domain.RevocationLift, RevocationEvidence) (domain.RevocationResult, error)
}

type RevocationLogEntry struct {
	EventID    domain.ID
	Sequence   int64
	Revocation domain.Revocation
	Change     domain.RevocationChangeType
	Epochs     domain.EpochVector
	OccurredAt time.Time
}

type GatewayCheckpoint struct {
	TenantID, GatewayPrincipalID           domain.ID
	LastSequence                           int64
	Epochs                                 domain.EpochVector
	LastStreamReceivedAt, LastReconciledAt *time.Time
	UpdatedAt                              time.Time
}

type RevocationSnapshot struct {
	Sequence int64
	Epochs   domain.EpochVector
	Active   []domain.Revocation
}

var ErrRevocationCursorGone = errors.New("revocation cursor is outside retention")

type RevocationDistributionRepository interface {
	RevocationChanges(context.Context, domain.ID, int64, int, time.Time) ([]RevocationLogEntry, error)
	RevocationSnapshot(context.Context, domain.ID, time.Time) (RevocationSnapshot, error)
	SaveGatewayCheckpoint(context.Context, GatewayCheckpoint) error
}

type RevocationAuthorityState struct {
	Epochs domain.EpochVector
	Active []domain.Revocation
}
type RevocationAuthority interface {
	AuthoritativeRevocations(context.Context, domain.ID, domain.ID, time.Time) (RevocationAuthorityState, error)
}
