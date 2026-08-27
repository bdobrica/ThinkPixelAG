package application

import (
	"context"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type revocationRepoStub struct {
	created domain.Revocation
	lifted  domain.RevocationLift
	calls   int
}

func (r *revocationRepoStub) CreateRevocation(_ context.Context, v domain.Revocation, _ ports.RevocationEvidence) (domain.RevocationResult, error) {
	r.created = v
	r.calls++
	return domain.RevocationResult{RevocationID: v.ID, Revocation: v, State: domain.RevocationCreated}, nil
}
func (r *revocationRepoStub) LiftRevocation(_ context.Context, v domain.RevocationLift, _ ports.RevocationEvidence) (domain.RevocationResult, error) {
	r.lifted = v
	r.calls++
	return domain.RevocationResult{RevocationID: v.RevocationID, State: domain.RevocationLifted}, nil
}

type revocationPolicyStub struct {
	allow bool
	input policy.Input
}

func (p *revocationPolicyStub) Decide(_ context.Context, in policy.Input) (policy.Result, error) {
	p.input = in
	return policy.Result{Decision: policy.Decision{DecisionID: in.DecisionID, Allow: p.allow, ReasonCodes: []string{"governance.operation.allowed"}}}, nil
}

func TestRevocationServiceAuthorizesCreateAndLift(t *testing.T) {
	tenant, _ := domain.NewID()
	actor, _ := domain.NewID()
	request, _ := domain.NewID()
	revocation, _ := domain.NewID()
	now := time.Unix(100, 0).UTC()
	repo := &revocationRepoStub{}
	eval := &revocationPolicyStub{allow: true}
	service, _ := NewRevocationService(repo, eval, fixedClock{now: now})
	command := ChangeRevocation{TenantID: tenant, PrincipalID: actor, RequestID: request, Scope: domain.RevocationGlobal, Target: "all", ReasonCode: "security.emergency", EffectiveAt: now, Roles: []string{"governance-admin"}, SecurityState: policy.SecurityState{Authoritative: true}}
	if _, err := service.Create(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if repo.created.TenantID != nil || eval.input.Subject.TenantID != tenant.String() || eval.input.Action != "revocations.create" {
		t.Fatalf("global authority/persistence scope mismatch: %+v %+v", repo.created, eval.input)
	}
	command.RevocationID = revocation
	command.ReasonCode = "security.restored"
	command.ApprovalReference = "approval:42"
	if _, err := service.Lift(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if repo.lifted.RevocationID != revocation || eval.input.Action != "revocations.lift" {
		t.Fatalf("lift not propagated: %+v", repo.lifted)
	}
	eval.allow = false
	if _, err := service.Create(context.Background(), command); domain.ErrorCodeOf(err) != domain.CodeForbidden || repo.calls != 2 {
		t.Fatalf("deny err=%v calls=%d", err, repo.calls)
	}
}
