package application

import (
	"context"
	"strings"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

type resolutionRepositoryStub struct {
	candidates []domain.AgentVersionCandidate
	persisted  domain.RunVersionResolution
}

func (stub *resolutionRepositoryStub) ListAgentVersionCandidates(context.Context, domain.ID, string) ([]domain.AgentVersionCandidate, error) {
	return stub.candidates, nil
}
func (stub *resolutionRepositoryStub) PersistRunVersionResolution(_ context.Context, resolution domain.RunVersionResolution) error {
	stub.persisted = resolution
	return nil
}
func (stub *resolutionRepositoryStub) DescribeRunVersionResolution(context.Context, domain.ID) (domain.RunVersionResolution, error) {
	return stub.persisted, nil
}

type resolutionEvaluatorStub struct {
	actions    []string
	allow      map[string]bool
	denyDigest string
}

func (stub *resolutionEvaluatorStub) Decide(_ context.Context, input policy.Input) (policy.Result, error) {
	stub.actions = append(stub.actions, input.Action)
	allowed := stub.allow[input.Action] && input.Resource.ID != stub.denyDigest
	return policy.Result{Decision: policy.Decision{DecisionID: input.DecisionID, Allow: allowed, ResolvedConstraints: map[string]any{"max_tokens": float64(10)}}, Metadata: policy.Metadata{PolicyDigest: "sha256:" + strings.Repeat("f", 64), PolicyVersion: 7}}, nil
}

func TestVersionResolverFallsBackToNextPolicyAllowedApprovedCandidate(t *testing.T) {
	t.Parallel()
	newest := resolutionCandidate(t, domain.AgentVersionApproved, "c")
	older := resolutionCandidate(t, domain.AgentVersionApproved, "d")
	older.Agent = newest.Agent
	older.Version.TenantID, older.Version.AgentID = newest.Agent.TenantID, newest.Agent.ID
	older.Approval.TenantID, older.Approval.AgentID, older.Approval.AgentVersionID = newest.Agent.TenantID, newest.Agent.ID, older.Version.ID
	evaluator := &resolutionEvaluatorStub{allow: map[string]bool{"runs.create": true}, denyDigest: newest.Version.ContentDigest}
	resolver, _ := NewVersionResolver(&resolutionRepositoryStub{candidates: []domain.AgentVersionCandidate{newest, older}}, evaluator, fixedClock{now: testTime()})
	resolution, err := resolver.Resolve(context.Background(), ResolveAgentVersion{RunID: applicationID(t), TenantID: newest.Agent.TenantID, AgentID: newest.Agent.ID, PrincipalID: applicationID(t), RequestID: applicationID(t), RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: policy.SecurityState{Authoritative: true}})
	if err != nil || resolution.AgentVersionID != older.Version.ID || len(evaluator.actions) != 2 {
		t.Fatalf("fallback=%+v actions=%v err=%v", resolution, evaluator.actions, err)
	}
}

func resolutionCandidate(t *testing.T, state domain.AgentVersionState, digestByte string) domain.AgentVersionCandidate {
	t.Helper()
	tenant, agentID, owner, sponsor, versionID, creator, approvalID, actor, policyID := applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t)
	now := testTime()
	manifest, _ := domain.NewAgentManifest("registry.example/agent@sha256:"+strings.Repeat(digestByte, 64), nil, nil, nil, nil, domain.AgentLimits{})
	digest, _ := manifest.ContentDigest()
	return domain.AgentVersionCandidate{Agent: domain.Agent{ID: agentID, TenantID: tenant, Name: "agent", OwnerPrincipalID: owner, SponsorPrincipalID: sponsor, RiskClass: domain.AgentRiskHigh, Status: domain.AgentActive, CreatedAt: now, UpdatedAt: now},
		Version:  domain.AgentVersion{ID: versionID, TenantID: tenant, AgentID: agentID, CreatedBy: creator, ContentDigest: digest, ImageDigest: "sha256:" + strings.Repeat(digestByte, 64), Manifest: manifest, CreatedAt: now},
		Approval: domain.AgentVersionApproval{ID: approvalID, TenantID: tenant, AgentID: agentID, AgentVersionID: versionID, ActorPrincipalID: actor, PolicyDecisionID: policyID, Decision: domain.DecisionApprove, ReasonCode: "registry.version.approved", CreatedAt: now}, State: state}
}

func TestVersionResolverAutomaticAndControlledSelection(t *testing.T) {
	t.Parallel()
	candidate := resolutionCandidate(t, domain.AgentVersionApproved, "a")
	repository := &resolutionRepositoryStub{candidates: []domain.AgentVersionCandidate{candidate}}
	evaluator := &resolutionEvaluatorStub{allow: map[string]bool{"runs.create": true, "versions.pin": true, "versions.rollback": true}}
	resolver, _ := NewVersionResolver(repository, evaluator, fixedClock{now: testTime()})
	command := ResolveAgentVersion{RunID: applicationID(t), TenantID: candidate.Agent.TenantID, AgentID: candidate.Agent.ID, PrincipalID: applicationID(t), RequestID: applicationID(t), RequestedConstraints: map[string]any{"max_tokens": float64(20)}, AuthorityConstraints: map[string]any{"max_tokens": float64(100)}, SecurityState: policy.SecurityState{Authoritative: true}}
	resolution, err := resolver.Resolve(context.Background(), command)
	if err != nil || resolution.Mode != domain.ResolutionAutomatic || len(evaluator.actions) != 1 || evaluator.actions[0] != "runs.create" {
		t.Fatalf("automatic resolution=%+v actions=%v err=%v", resolution, evaluator.actions, err)
	}
	if err := resolver.Persist(context.Background(), resolution); err != nil || repository.persisted.AgentVersionID != candidate.Version.ID {
		t.Fatalf("persist=%+v err=%v", repository.persisted, err)
	}

	evaluator.actions = nil
	command.RequestedVersionDigest = candidate.Version.ContentDigest
	resolution, err = resolver.Resolve(context.Background(), command)
	if err != nil || resolution.Mode != domain.ResolutionPinned || resolution.SelectionDecisionID.IsZero() || strings.Join(evaluator.actions, ",") != "versions.pin,runs.create" {
		t.Fatalf("pin resolution=%+v actions=%v err=%v", resolution, evaluator.actions, err)
	}

	candidate.State = domain.AgentVersionDeprecated
	repository.candidates = []domain.AgentVersionCandidate{candidate}
	evaluator.actions = nil
	resolution, err = resolver.Resolve(context.Background(), command)
	if err != nil || resolution.Mode != domain.ResolutionRollback || strings.Join(evaluator.actions, ",") != "versions.rollback,runs.create" {
		t.Fatalf("rollback resolution=%+v actions=%v err=%v", resolution, evaluator.actions, err)
	}
}

func TestVersionResolverFailsClosedForUnauthorizedPin(t *testing.T) {
	t.Parallel()
	candidate := resolutionCandidate(t, domain.AgentVersionApproved, "b")
	resolver, _ := NewVersionResolver(&resolutionRepositoryStub{candidates: []domain.AgentVersionCandidate{candidate}}, &resolutionEvaluatorStub{allow: map[string]bool{"runs.create": true}}, fixedClock{now: testTime()})
	_, err := resolver.Resolve(context.Background(), ResolveAgentVersion{RunID: applicationID(t), TenantID: candidate.Agent.TenantID, AgentID: candidate.Agent.ID, PrincipalID: applicationID(t), RequestID: applicationID(t), RequestedVersionDigest: candidate.Version.ContentDigest, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: policy.SecurityState{Authoritative: true}})
	if domain.ErrorCodeOf(err) != domain.CodeForbidden {
		t.Fatalf("pin denial=%v", err)
	}
}
