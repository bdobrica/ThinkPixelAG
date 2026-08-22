package application

import (
	"context"
	"errors"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type runQueryRepositoryStub struct {
	record ports.RunAccessRecord
	err    error
}

func (s runQueryRepositoryStub) GetRun(context.Context, domain.ID) (ports.RunAccessRecord, error) {
	return s.record, s.err
}

type runQueryEvaluatorStub struct {
	allow bool
	input policy.Input
	err   error
}

func (s *runQueryEvaluatorStub) Decide(_ context.Context, input policy.Input) (policy.Result, error) {
	s.input = input
	if s.err != nil {
		return policy.Result{}, s.err
	}
	return policy.Result{Decision: policy.Decision{DecisionID: input.DecisionID, Allow: s.allow}}, nil
}

func TestRunQueryAuthorizesTenantScopedProjection(t *testing.T) {
	tenant, principal, request, runID, agent, version, owner := applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t)
	now := testTime()
	run := domain.Run{ID: runID, TenantID: tenant, AgentID: agent, AgentVersionID: version, RequestedBy: principal, VersionDigest: "sha256:" + repeat("a", 64), State: domain.RunAdmitted, StateVersion: 1, EnvelopeVersion: 1, CreatedAt: now, UpdatedAt: now}
	evaluator := &runQueryEvaluatorStub{allow: true}
	service, _ := NewRunQuery(runQueryRepositoryStub{record: ports.RunAccessRecord{Run: run, AgentRiskClass: domain.AgentRiskMedium, AgentOwnerID: owner}}, evaluator, fixedClock{now: now})
	got, err := service.Get(context.Background(), GetRun{TenantID: tenant, PrincipalID: principal, RequestID: request, RunID: runID, Roles: []string{"agent-invoker"}})
	if err != nil || got.ID != runID || evaluator.input.Action != "runs.read" || evaluator.input.Resource.ID != runID.String() || evaluator.input.Resource.TenantID != tenant.String() {
		t.Fatalf("got=%+v err=%v input=%+v", got, err, evaluator.input)
	}
}

func TestRunQueryMakesMissingAndDeniedEnumerationSafe(t *testing.T) {
	tenant, principal, request, runID, agent, version, owner := applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t)
	command := GetRun{TenantID: tenant, PrincipalID: principal, RequestID: request, RunID: runID}
	missing, _ := NewRunQuery(runQueryRepositoryStub{err: domain.NewError(domain.CodeNotFound, "database detail")}, &runQueryEvaluatorStub{}, fixedClock{now: testTime()})
	_, missingErr := missing.Get(context.Background(), command)
	run := domain.Run{ID: runID, TenantID: tenant, AgentID: agent, AgentVersionID: version, RequestedBy: principal, VersionDigest: "sha256:" + repeat("b", 64), State: domain.RunAdmitted, StateVersion: 1, EnvelopeVersion: 1, CreatedAt: testTime(), UpdatedAt: testTime()}
	denied, _ := NewRunQuery(runQueryRepositoryStub{record: ports.RunAccessRecord{Run: run, AgentRiskClass: domain.AgentRiskLow, AgentOwnerID: owner}}, &runQueryEvaluatorStub{allow: false}, fixedClock{now: testTime()})
	_, deniedErr := denied.Get(context.Background(), command)
	for _, err := range []error{missingErr, deniedErr} {
		var typed *domain.Error
		if !errors.As(err, &typed) || typed.Code() != domain.CodeNotFound || typed.Detail() != "run not found" {
			t.Fatalf("unsafe error: %v", err)
		}
	}
}

func TestRunQueryFailsClosedWhenPolicyIsUnavailable(t *testing.T) {
	tenant, principal, request, runID, agent, version, owner := applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t)
	now := testTime()
	run := domain.Run{ID: runID, TenantID: tenant, AgentID: agent, AgentVersionID: version, RequestedBy: principal, VersionDigest: "sha256:" + repeat("d", 64), State: domain.RunAdmitted, StateVersion: 1, EnvelopeVersion: 1, CreatedAt: now, UpdatedAt: now}
	service, _ := NewRunQuery(runQueryRepositoryStub{record: ports.RunAccessRecord{Run: run, AgentRiskClass: domain.AgentRiskLow, AgentOwnerID: owner}}, &runQueryEvaluatorStub{err: errors.New("opa unavailable")}, fixedClock{now: now})
	_, err := service.Get(context.Background(), GetRun{TenantID: tenant, PrincipalID: principal, RequestID: request, RunID: runID})
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code() != domain.CodeUnavailable || !typed.Retryable() {
		t.Fatalf("policy outage error=%v", err)
	}
}

func repeat(value string, count int) string {
	out := ""
	for range count {
		out += value
	}
	return out
}
