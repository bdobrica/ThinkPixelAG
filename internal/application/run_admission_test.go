package application

import (
	"context"
	"strings"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type admissionRepositoryStub struct {
	admission  domain.RunAdmission
	resolution domain.RunVersionResolution
	evidence   ports.RunAdmissionEvidence
	calls      int
}

func (stub *admissionRepositoryStub) AdmitRun(_ context.Context, admission domain.RunAdmission, resolution domain.RunVersionResolution, evidence ports.RunAdmissionEvidence) error {
	stub.admission, stub.resolution, stub.evidence = admission, resolution, evidence
	stub.calls++
	return nil
}

func TestRunAdmissionAuthenticatesAuthorizesNarrowsAndPersists(t *testing.T) {
	t.Parallel()
	candidate := resolutionCandidate(t, domain.AgentVersionApproved, "9")
	resolver, _ := NewVersionResolver(&resolutionRepositoryStub{candidates: []domain.AgentVersionCandidate{candidate}}, &resolutionEvaluatorStub{allow: map[string]bool{"runs.create": true}}, fixedClock{now: testTime()})
	repository := &admissionRepositoryStub{}
	service, _ := NewRunAdmissionService(resolver, repository, fixedClock{now: testTime()})
	command := AdmitRun{TenantID: candidate.Agent.TenantID, PrincipalID: applicationID(t), AgentID: candidate.Agent.ID, RequestID: applicationID(t), Roles: []string{"invoker"}, RequestedConstraints: map[string]any{"max_tokens": float64(20)}, AuthorityConstraints: map[string]any{"max_tokens": float64(100)}, SecurityState: policy.SecurityState{Authoritative: true}}
	// Add a deadline to the narrowed result to prove that run timing derives
	// from policy output.
	resolver.evaluator = policyEvaluatorFunc(func(_ context.Context, input policy.Input) (policy.Result, error) {
		return policy.Result{Decision: policy.Decision{DecisionID: input.DecisionID, Allow: true, ResolvedConstraints: map[string]any{"max_tokens": float64(10), "max_execution_time_seconds": float64(60)}}, Metadata: policy.Metadata{PolicyDigest: "sha256:" + strings.Repeat("f", 64), PolicyVersion: 7}}, nil
	})
	admission, err := service.Admit(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	if repository.calls != 1 || repository.resolution.RunID != admission.RunID || repository.evidence.RequestID != command.RequestID {
		t.Fatalf("repository admission=%+v resolution=%+v evidence=%+v", repository.admission, repository.resolution, repository.evidence)
	}
	if admission.State != domain.RunAdmitted || admission.StateVersion != 1 || admission.Constraints["max_tokens"] != float64(10) || admission.DeadlineAt == nil || admission.DeadlineAt.Sub(admission.CreatedAt).Seconds() != 60 {
		t.Fatalf("admission=%+v", admission)
	}
}

type policyEvaluatorFunc func(context.Context, policy.Input) (policy.Result, error)

func (fn policyEvaluatorFunc) Decide(ctx context.Context, input policy.Input) (policy.Result, error) {
	return fn(ctx, input)
}

func TestRunAdmissionFailsBeforePersistenceWithoutAuthenticatedIdentityOrPolicyAllow(t *testing.T) {
	t.Parallel()
	candidate := resolutionCandidate(t, domain.AgentVersionApproved, "8")
	repository := &admissionRepositoryStub{}
	resolver, _ := NewVersionResolver(&resolutionRepositoryStub{candidates: []domain.AgentVersionCandidate{candidate}}, &resolutionEvaluatorStub{allow: map[string]bool{}}, fixedClock{now: testTime()})
	service, _ := NewRunAdmissionService(resolver, repository, fixedClock{now: testTime()})
	command := AdmitRun{TenantID: candidate.Agent.TenantID, AgentID: candidate.Agent.ID, RequestID: applicationID(t), RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}}
	if _, err := service.Admit(context.Background(), command); domain.ErrorCodeOf(err) != domain.CodeUnauthenticated {
		t.Fatalf("missing identity error=%v", err)
	}
	command.PrincipalID = applicationID(t)
	if _, err := service.Admit(context.Background(), command); domain.ErrorCodeOf(err) != domain.CodeForbidden {
		t.Fatalf("policy denial error=%v", err)
	}
	if repository.calls != 0 {
		t.Fatalf("repository called %d times", repository.calls)
	}
}
