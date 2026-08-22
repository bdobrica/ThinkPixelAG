package application

import (
	"context"
	"errors"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type cancellationRepositoryStub struct {
	record       ports.RunAccessRecord
	cancellation domain.RunCancellation
	evidence     ports.RunCancellationEvidence
	result       domain.Run
	err          error
}

func (stub *cancellationRepositoryStub) GetRun(context.Context, domain.ID) (ports.RunAccessRecord, error) {
	return stub.record, stub.err
}

func (stub *cancellationRepositoryStub) CancelRun(_ context.Context, cancellation domain.RunCancellation, evidence ports.RunCancellationEvidence) (domain.Run, error) {
	stub.cancellation, stub.evidence = cancellation, evidence
	return stub.result, stub.err
}

func TestRunCancellationAuthorizesAndPersistsEvidence(t *testing.T) {
	tenant, principal, request, runID, agent, version, owner := applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t)
	now := testTime()
	run := domain.Run{ID: runID, TenantID: tenant, AgentID: agent, AgentVersionID: version, RequestedBy: principal, VersionDigest: "sha256:" + repeat("a", 64), State: domain.RunRunning, StateVersion: 4, EnvelopeVersion: 1, CreatedAt: now, UpdatedAt: now}
	repository := &cancellationRepositoryStub{record: ports.RunAccessRecord{Run: run, AgentRiskClass: domain.AgentRiskHigh, AgentOwnerID: owner}, result: run}
	evaluator := &runQueryEvaluatorStub{allow: true}
	service, _ := NewRunCancellationService(repository, evaluator, fixedClock{now: now})
	expected := int64(4)
	_, err := service.Cancel(context.Background(), CancelRun{TenantID: tenant, PrincipalID: principal, RequestID: request, RunID: runID, Roles: []string{"agent-invoker"}, IdempotencyKey: "abcdefghijklmnop", ReasonCode: "caller.request", ExpectedStateVersion: &expected})
	if err != nil || evaluator.input.Action != "runs.cancel" || evaluator.input.Resource.Attributes["state"] != "RUNNING" || repository.cancellation.IdempotencyKey != "cancel:abcdefghijklmnop" || repository.evidence.PolicyDecisionID.IsZero() {
		t.Fatalf("error=%v input=%+v cancellation=%+v evidence=%+v", err, evaluator.input, repository.cancellation, repository.evidence)
	}
}

func TestRunCancellationDenyAndOutageFailClosed(t *testing.T) {
	tenant, principal, request, runID, agent, version, owner := applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t)
	now := testTime()
	run := domain.Run{ID: runID, TenantID: tenant, AgentID: agent, AgentVersionID: version, RequestedBy: principal, VersionDigest: "sha256:" + repeat("b", 64), State: domain.RunRunning, StateVersion: 1, EnvelopeVersion: 1, CreatedAt: now, UpdatedAt: now}
	command := CancelRun{TenantID: tenant, PrincipalID: principal, RequestID: request, RunID: runID, IdempotencyKey: "abcdefghijklmnop"}
	for _, test := range []struct {
		name      string
		evaluator *runQueryEvaluatorStub
		code      domain.ErrorCode
	}{{"deny", &runQueryEvaluatorStub{}, domain.CodeNotFound}, {"outage", &runQueryEvaluatorStub{err: errors.New("opa down")}, domain.CodeUnavailable}} {
		t.Run(test.name, func(t *testing.T) {
			service, _ := NewRunCancellationService(&cancellationRepositoryStub{record: ports.RunAccessRecord{Run: run, AgentRiskClass: domain.AgentRiskLow, AgentOwnerID: owner}}, test.evaluator, fixedClock{now: now})
			_, err := service.Cancel(context.Background(), command)
			if domain.ErrorCodeOf(err) != test.code {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
