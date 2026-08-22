package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

type approvalRepositoryStub struct {
	recorded domain.AgentVersionApproval
}

type policyEvaluatorStub struct {
	input policy.Input
	allow bool
}

func (stub *policyEvaluatorStub) Decide(_ context.Context, input policy.Input) (policy.Result, error) {
	stub.input = input
	return policy.Result{Decision: policy.Decision{DecisionID: input.DecisionID, Allow: stub.allow}}, nil
}

func (stub *approvalRepositoryStub) RecordAgentVersionDecision(_ context.Context, approval domain.AgentVersionApproval, _ string, _, _ domain.ID, _ *domain.ID) (domain.AgentVersionApproval, error) {
	approval.AgentVersionID, _ = domain.NewID()
	stub.recorded = approval
	return approval, nil
}

func TestPolicyAgentApprovalAuthorizerRequiresExplicitAllow(t *testing.T) {
	t.Parallel()
	evaluator := &policyEvaluatorStub{allow: true}
	authorizer, err := NewPolicyAgentApprovalAuthorizer(evaluator, func() time.Time { return testTime() })
	if err != nil {
		t.Fatal(err)
	}
	request := AgentVersionDecisionAuthorization{TenantID: applicationID(t), ActorPrincipalID: applicationID(t), AgentID: applicationID(t), VersionDigest: "sha256:" + strings.Repeat("a", 64), Decision: domain.DecisionDeprecate, Roles: []string{"governance-admin"}, RequestID: applicationID(t)}
	if _, err := authorizer.AuthorizeAgentVersionDecision(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if evaluator.input.Action != "versions.approve" || evaluator.input.Resource.Attributes["decision"] != "deprecated" || !evaluator.input.SecurityState.Authoritative {
		t.Fatalf("policy input = %+v", evaluator.input)
	}
	evaluator.allow = false
	if _, err := authorizer.AuthorizeAgentVersionDecision(context.Background(), request); domain.ErrorCodeOf(err) != domain.CodeForbidden {
		t.Fatalf("denial error = %v", err)
	}
}
func (*approvalRepositoryStub) AgentVersionEligibility(context.Context, domain.ID, string) (domain.AgentVersionState, error) {
	return domain.AgentVersionApproved, nil
}

type approvalAuthorizerStub struct {
	decisionID domain.ID
	err        error
	request    AgentVersionDecisionAuthorization
}

func (stub *approvalAuthorizerStub) AuthorizeAgentVersionDecision(_ context.Context, request AgentVersionDecisionAuthorization) (domain.ID, error) {
	stub.request = request
	return stub.decisionID, stub.err
}

func TestAgentApprovalRegistryAuthorizesAndRecordsEvidenceBoundDecision(t *testing.T) {
	t.Parallel()
	repository := &approvalRepositoryStub{}
	authorizer := &approvalAuthorizerStub{decisionID: applicationID(t)}
	service, err := NewAgentApprovalRegistry(repository, authorizer, fixedClock{now: testTime()})
	if err != nil {
		t.Fatal(err)
	}
	command := DecideAgentVersion{ID: applicationID(t), TenantID: applicationID(t), AgentID: applicationID(t), ActorPrincipalID: applicationID(t), RequestID: applicationID(t),
		VersionDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Decision: domain.DecisionApprove, ReasonCode: "registry.version.approved", ApprovalReference: "change-42", Roles: []string{"registry-admin"}}
	approval, err := service.Decide(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if approval.PolicyDecisionID != authorizer.decisionID || repository.recorded.PolicyDecisionID != authorizer.decisionID || authorizer.request.Decision != domain.DecisionApprove {
		t.Fatalf("approval=%+v authorization=%+v", approval, authorizer.request)
	}

	denied := &approvalAuthorizerStub{err: domain.NewError(domain.CodeForbidden, "denied")}
	service, _ = NewAgentApprovalRegistry(repository, denied, fixedClock{now: testTime()})
	if _, err := service.Decide(context.Background(), command); domain.ErrorCodeOf(err) != domain.CodeForbidden {
		t.Fatalf("denial error = %v", err)
	}
}
