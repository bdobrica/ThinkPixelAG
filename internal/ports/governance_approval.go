package ports

import (
	"context"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

// ApprovalProvider isolates vendor-specific approval systems from governance
// semantics. Assertions must be bound to the request reference and digest.
type ApprovalProvider interface {
	RequestApproval(context.Context, ApprovalChallenge) (string, error)
	VerifyApproval(context.Context, ApprovalAssertion) error
}

type ApprovalChallenge struct {
	ApprovalID, TenantID, RequesterPrincipalID domain.ID
	Action                                     domain.GovernanceApprovalAction
	ResourceType, ResourceID, RequestDigest    string
	ExpiresAt                                  time.Time
}

type ApprovalAssertion struct {
	ProviderReference, DecisionReference string
	ApprovalID, ApproverPrincipalID      domain.ID
	RequestDigest                        string
	Approved                             bool
	DecidedAt                            time.Time
}

type GovernanceApprovalStore interface {
	CreateGovernanceApproval(context.Context, domain.GovernanceApproval) error
	GovernanceApproval(context.Context, domain.ID) (domain.GovernanceApproval, error)
	RecordGovernanceApprovalDecision(context.Context, domain.GovernanceApproval) error
	ConsumeGovernanceApproval(context.Context, domain.ID, string, time.Time) (domain.GovernanceApproval, error)
}
