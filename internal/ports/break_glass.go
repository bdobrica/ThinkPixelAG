package ports

import (
	"context"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
)

type StrongAuthenticationResult struct {
	TenantID, PrincipalID domain.ID
	Reference             string
	AuthenticatedAt       time.Time
}

// StrongAuthenticationVerifier keeps IdP-specific step-up assertions outside
// the domain. Successful verification must mean phishing-resistant MFA.
type StrongAuthenticationVerifier interface {
	VerifyStrongAuthentication(context.Context, string) (StrongAuthenticationResult, error)
}

type BreakGlassStore interface {
	// Activate atomically consumes the digest-bound approval and appends the
	// grant plus its separate exportable activation evidence.
	ActivateBreakGlass(context.Context, domain.BreakGlassGrant, evidence.Event) error
	BreakGlass(context.Context, domain.ID, domain.ID) (domain.BreakGlassGrant, error)
	UseBreakGlass(context.Context, domain.ID, domain.ID, domain.ID, domain.BreakGlassScope, string, string, string, time.Time, evidence.Event) error
	RevokeBreakGlass(context.Context, domain.ID, domain.ID, domain.ID, time.Time, evidence.Event) error
	ExpireBreakGlass(context.Context, domain.ID, domain.ID, time.Time, evidence.Event) error
}
