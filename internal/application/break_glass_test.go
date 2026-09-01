package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type breakGlassAuthStub struct {
	result ports.StrongAuthenticationResult
	err    error
}

func (s breakGlassAuthStub) VerifyStrongAuthentication(context.Context, string) (ports.StrongAuthenticationResult, error) {
	return s.result, s.err
}

type breakGlassStoreStub struct {
	grant     domain.BreakGlassGrant
	activated evidence.Event
	used      evidence.Event
	uses      int
}

func (s *breakGlassStoreStub) ActivateBreakGlass(_ context.Context, g domain.BreakGlassGrant, e evidence.Event) error {
	s.grant = g
	s.activated = e
	return nil
}
func (s *breakGlassStoreStub) BreakGlass(context.Context, domain.ID, domain.ID) (domain.BreakGlassGrant, error) {
	return s.grant, nil
}
func (s *breakGlassStoreStub) UseBreakGlass(_ context.Context, _ domain.ID, _ domain.ID, _ domain.ID, _ domain.BreakGlassScope, _, _, _ string, _ time.Time, e evidence.Event) error {
	s.uses++
	s.used = e
	return nil
}
func (s *breakGlassStoreStub) RevokeBreakGlass(context.Context, domain.ID, domain.ID, domain.ID, time.Time, evidence.Event) error {
	return nil
}
func (s *breakGlassStoreStub) ExpireBreakGlass(context.Context, domain.ID, domain.ID, time.Time, evidence.Event) error {
	return nil
}

func TestBreakGlassRequiresRecentStrongAuthenticationAndEmitsSeparateEvidence(t *testing.T) {
	ids := make([]domain.ID, 4)
	for i := range ids {
		ids[i], _ = domain.NewID()
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	store := &breakGlassStoreStub{}
	service, _ := NewBreakGlassService(store, breakGlassAuthStub{result: ports.StrongAuthenticationResult{TenantID: ids[1], PrincipalID: ids[2], Reference: "webauthn:1", AuthenticatedAt: now.Add(-time.Minute)}}, fixedClock{now})
	credential, err := service.Activate(context.Background(), ActivateBreakGlass{ID: ids[0], TenantID: ids[1], PrincipalID: ids[2], ApprovalID: ids[3], Scope: domain.BreakGlassPolicyRecovery, ResourceType: "policy_channel", ResourceID: "stable", ReasonCode: "security.recovery", StrongAuthenticationAssertion: "opaque", ExpiresAt: now.Add(10 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if credential.Token == "" || store.grant.CredentialDigest == "" || store.activated.EventType != evidence.BreakGlass {
		t.Fatal("activation omitted credential digest or evidence")
	}
	if err := service.Use(context.Background(), ids[1], ids[2], ids[0], domain.BreakGlassPolicyRecovery, "policy_channel", "stable", credential.Token); err != nil {
		t.Fatal(err)
	}
	if store.uses != 1 || store.used.Action != "break_glass.use" {
		t.Fatal("use did not produce separate evidence")
	}
	stale, _ := NewBreakGlassService(&breakGlassStoreStub{}, breakGlassAuthStub{result: ports.StrongAuthenticationResult{TenantID: ids[1], PrincipalID: ids[2], Reference: "webauthn:old", AuthenticatedAt: now.Add(-6 * time.Minute)}}, fixedClock{now})
	if _, err := stale.Activate(context.Background(), ActivateBreakGlass{ID: ids[0], TenantID: ids[1], PrincipalID: ids[2], ApprovalID: ids[3], Scope: domain.BreakGlassPolicyRecovery, ResourceType: "policy", ResourceID: "stable", ReasonCode: "recovery", ExpiresAt: now.Add(time.Minute)}); err == nil {
		t.Fatal("stale strong authentication accepted")
	}
}

func TestBreakGlassFailsClosedWhenAuthenticatorFails(t *testing.T) {
	service, _ := NewBreakGlassService(&breakGlassStoreStub{}, breakGlassAuthStub{err: errors.New("down")}, fixedClock{time.Now().UTC()})
	if _, err := service.Activate(context.Background(), ActivateBreakGlass{}); err == nil {
		t.Fatal("authenticator outage accepted")
	}
}
