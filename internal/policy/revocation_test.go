package policy

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"testing"
	"time"
)

type authorityStub struct {
	state ports.RevocationAuthorityState
}

func (s authorityStub) AuthoritativeRevocations(context.Context, domain.ID, domain.ID, time.Time) (ports.RevocationAuthorityState, error) {
	return s.state, nil
}

type evaluatorStub struct{ called bool }

func (s *evaluatorStub) Decide(_ context.Context, in Input) (Result, error) {
	s.called = true
	return Result{Decision: Decision{DecisionID: in.DecisionID, Allow: true}}, nil
}
func TestRevocationEvaluatorEnforcesScopeAndEpochs(t *testing.T) {
	tenant, _ := domain.NewID()
	principal, _ := domain.NewID()
	rev, _ := domain.NewID()
	actor, _ := domain.NewID()
	now := time.Now().UTC()
	next := &evaluatorStub{}
	wrapper, _ := NewRevocationEvaluator(next, authorityStub{ports.RevocationAuthorityState{Epochs: domain.EpochVector{Security: 7, TenantPolicy: 3, TenantRevocation: 4}, Active: []domain.Revocation{{ID: rev, TenantID: &tenant, ActorPrincipalID: actor, Scope: domain.RevocationPrincipalID, Target: principal.String(), ReasonCode: "security.compromise", EffectiveAt: now, CreatedAt: now}}}}, func() time.Time { return now })
	in := Input{DecisionID: "decision", Subject: Subject{TenantID: tenant.String(), PrincipalID: principal.String()}, SecurityState: SecurityState{Authoritative: true}}
	got, err := wrapper.Decide(context.Background(), in)
	if err != nil || got.Decision.Allow || got.Decision.ReasonCodes[0] != "principal.revoked" || next.called {
		t.Fatalf("result=%+v called=%v err=%v", got, next.called, err)
	}
	next.called = false
	wrapper, _ = NewRevocationEvaluator(next, authorityStub{ports.RevocationAuthorityState{Epochs: domain.EpochVector{Security: 7, TenantPolicy: 3, TenantRevocation: 4}}}, func() time.Time { return now })
	in.Subject.PrincipalID = actor.String()
	in.SecurityState = SecurityState{GlobalEpoch: 6, TenantPolicyEpoch: 3, TenantRevocationEpoch: 4}
	got, err = wrapper.Decide(context.Background(), in)
	if err != nil || got.Decision.Allow || got.Decision.ReasonCodes[0] != "security_state.stale" || next.called {
		t.Fatalf("stale result=%+v called=%v err=%v", got, next.called, err)
	}
}
