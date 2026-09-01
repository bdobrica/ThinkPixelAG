package application

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const maximumStrongAuthenticationAge = 5 * time.Minute

type BreakGlassService struct {
	store    ports.BreakGlassStore
	verifier ports.StrongAuthenticationVerifier
	clock    domain.Clock
}

func NewBreakGlassService(store ports.BreakGlassStore, verifier ports.StrongAuthenticationVerifier, clock domain.Clock) (*BreakGlassService, error) {
	if store == nil || verifier == nil || clock == nil {
		return nil, errors.New("break-glass service requires store, strong authenticator, and clock")
	}
	return &BreakGlassService{store: store, verifier: verifier, clock: clock}, nil
}

type ActivateBreakGlass struct {
	ID, TenantID, PrincipalID, ApprovalID                               domain.ID
	Scope                                                               domain.BreakGlassScope
	ResourceType, ResourceID, ReasonCode, StrongAuthenticationAssertion string
	ExpiresAt                                                           time.Time
}

type BreakGlassCredential struct {
	Grant domain.BreakGlassGrant
	Token string
}

func (service *BreakGlassService) Activate(ctx context.Context, command ActivateBreakGlass) (BreakGlassCredential, error) {
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return BreakGlassCredential{}, domain.WrapError(domain.CodeInternal, "break-glass clock is invalid", err)
	}
	authentication, err := service.verifier.VerifyStrongAuthentication(ctx, command.StrongAuthenticationAssertion)
	if err != nil {
		return BreakGlassCredential{}, domain.WrapError(domain.CodeForbidden, "strong authentication failed", err)
	}
	authenticatedAt, utcErr := domain.RequireUTC(authentication.AuthenticatedAt)
	if authentication.TenantID != command.TenantID || authentication.PrincipalID != command.PrincipalID || authentication.Reference == "" || authentication.AuthenticatedAt.After(now) || now.Sub(authentication.AuthenticatedAt) > maximumStrongAuthenticationAge {
		return BreakGlassCredential{}, domain.NewError(domain.CodeForbidden, "strong authentication is stale or identity-bound incorrectly")
	}
	if utcErr != nil {
		return BreakGlassCredential{}, domain.NewError(domain.CodeForbidden, "strong authentication time is invalid")
	}
	authentication.AuthenticatedAt = authenticatedAt
	digest, err := domain.BreakGlassGrantDigest(command.TenantID, command.PrincipalID, command.Scope, command.ResourceType, command.ResourceID, command.ReasonCode, command.ExpiresAt)
	if err != nil {
		return BreakGlassCredential{}, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return BreakGlassCredential{}, domain.WrapError(domain.CodeInternal, "create break-glass credential", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	grant := domain.BreakGlassGrant{ID: command.ID, TenantID: command.TenantID, PrincipalID: command.PrincipalID, ApprovalID: command.ApprovalID, Scope: command.Scope, ResourceType: command.ResourceType, ResourceID: command.ResourceID, ReasonCode: command.ReasonCode, GrantDigest: digest, CredentialDigest: domain.DigestBreakGlassCredential(token), StrongAuthenticationReference: authentication.Reference, IssuedAt: now, ExpiresAt: command.ExpiresAt}
	if err := grant.Validate(); err != nil {
		return BreakGlassCredential{}, domain.WrapError(domain.CodeInvalidArgument, "break-glass grant is invalid", err)
	}
	event, err := breakGlassEvent(grant, "break_glass.activate", "ACTIVATED", now)
	if err != nil {
		return BreakGlassCredential{}, err
	}
	if err := service.store.ActivateBreakGlass(ctx, grant, event); err != nil {
		return BreakGlassCredential{}, err
	}
	return BreakGlassCredential{Grant: grant, Token: token}, nil
}

func (service *BreakGlassService) Use(ctx context.Context, tenantID, principalID, grantID domain.ID, scope domain.BreakGlassScope, resourceType, resourceID, token string) error {
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return err
	}
	grant, err := service.store.BreakGlass(ctx, tenantID, grantID)
	if err != nil {
		return err
	}
	if grant.PrincipalID != principalID || grant.Scope != scope || grant.ResourceType != resourceType || grant.ResourceID != resourceID {
		return domain.NewError(domain.CodeForbidden, "break-glass grant is not bound to this operation")
	}
	event, err := breakGlassEvent(grant, "break_glass.use", "USED", now)
	if err != nil {
		return err
	}
	return service.store.UseBreakGlass(ctx, tenantID, principalID, grantID, scope, resourceType, resourceID, domain.DigestBreakGlassCredential(token), now, event)
}

func (service *BreakGlassService) Revoke(ctx context.Context, tenantID, principalID, grantID domain.ID) error {
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return err
	}
	grant, err := service.store.BreakGlass(ctx, tenantID, grantID)
	if err != nil {
		return err
	}
	event, err := breakGlassEvent(grant, "break_glass.revoke", "REVOKED", now)
	if err != nil {
		return err
	}
	return service.store.RevokeBreakGlass(ctx, tenantID, principalID, grantID, now, event)
}

func (service *BreakGlassService) Expire(ctx context.Context, tenantID, grantID domain.ID) error {
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return err
	}
	grant, err := service.store.BreakGlass(ctx, tenantID, grantID)
	if err != nil {
		return err
	}
	event, err := breakGlassEvent(grant, "break_glass.expire", "EXPIRED", now)
	if err != nil {
		return err
	}
	event.Actor = evidence.Actor{Type: "SYSTEM", ID: "thinkpixelag"}
	return service.store.ExpireBreakGlass(ctx, tenantID, grantID, now, event)
}

func breakGlassEvent(grant domain.BreakGlassGrant, action, change string, at time.Time) (evidence.Event, error) {
	id, err := domain.NewID()
	if err != nil {
		return evidence.Event{}, err
	}
	return evidence.New(evidence.Event{ID: id, EventType: evidence.BreakGlass, TenantID: &grant.TenantID, Actor: evidence.Actor{Type: "PRINCIPAL", ID: grant.PrincipalID.String()}, Action: action, Outcome: evidence.Succeeded, ReasonCodes: []string{"security.break_glass"}, OccurredAt: at}, evidence.BreakGlassData{SessionID: grant.ID.String(), Scope: string(grant.Scope), GrantDigest: grant.GrantDigest, ApprovalID: grant.ApprovalID.String(), ExpiresAt: grant.ExpiresAt, Change: change})
}
