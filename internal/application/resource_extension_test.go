package application

import (
	"context"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type extensionRepoStub struct {
	record    ports.RunAccessRecord
	extension domain.ResourceExtension
	evidence  ports.ResourceExtensionEvidence
	calls     int
}

func (r *extensionRepoStub) GetRun(context.Context, domain.ID) (ports.RunAccessRecord, error) {
	return r.record, nil
}
func (r *extensionRepoStub) ExtendResources(_ context.Context, e domain.ResourceExtension, v ports.ResourceExtensionEvidence) (domain.ResourceExtensionResult, error) {
	r.calls++
	r.extension = e
	r.evidence = v
	return domain.ResourceExtensionResult{ID: e.ID, EnvelopeVersion: 2}, nil
}

type extensionPolicyStub struct {
	input policy.Input
	allow bool
}

func (p *extensionPolicyStub) Decide(_ context.Context, in policy.Input) (policy.Result, error) {
	p.input = in
	return policy.Result{Decision: policy.Decision{DecisionID: in.DecisionID, Allow: p.allow, ReasonCodes: []string{"resource.extension.approved"}}}, nil
}

func TestResourceExtensionAuthorizesAndPreventsSelfExtension(t *testing.T) {
	ids := make([]domain.ID, 8)
	for i := range ids {
		ids[i], _ = domain.NewID()
	}
	now := time.Unix(20, 0).UTC()
	run := domain.Run{ID: ids[0], TenantID: ids[1], AgentID: ids[2], AgentVersionID: ids[3], RequestedBy: ids[4], VersionDigest: "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", State: domain.RunPausedForBudget, StateVersion: 3, EnvelopeVersion: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute)}
	repo := &extensionRepoStub{record: ports.RunAccessRecord{Run: run, AgentRiskClass: domain.AgentRiskHigh, AgentOwnerID: ids[5]}}
	eval := &extensionPolicyStub{allow: true}
	service, _ := NewResourceExtensionService(repo, eval, fixedClock{now: now})
	amount, _ := domain.NewDecimal(10, 0)
	quantity, _ := domain.NewQuantity(amount, "llm_tokens")
	command := ExtendResources{TenantID: ids[1], PrincipalID: ids[6], RequestID: ids[7], RunID: ids[0], Roles: []string{"governance-admin"}, IdempotencyKey: "extend-1", ReasonCode: "budget.increase", ApprovalReference: "CAB-42", Additions: []domain.ResourceExtensionAmount{{Name: "llm_tokens", Quantity: quantity}}, SecurityState: policy.SecurityState{Authoritative: true}}
	if _, err := service.Extend(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || eval.input.Action != "resources.extend" || repo.extension.ActorPrincipalID != ids[6] || repo.extension.PolicyDecisionID.IsZero() || repo.evidence.AuditID.IsZero() {
		t.Fatalf("extension/evidence not propagated: %+v %+v", repo.extension, repo.evidence)
	}
	command.PrincipalID = ids[4]
	if _, err := service.Extend(context.Background(), command); domain.ErrorCodeOf(err) != domain.CodeForbidden || repo.calls != 1 {
		t.Fatalf("self extension err=%v calls=%d", err, repo.calls)
	}
}
