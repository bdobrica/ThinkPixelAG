package application

import (
	"context"
	"errors"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type signalRepositoryStub struct {
	record   ports.RunAccessRecord
	event    domain.RunEvent
	signal   domain.RunSignal
	evidence ports.RunSignalEvidence
	err      error
}

func (s *signalRepositoryStub) GetRun(context.Context, domain.ID) (ports.RunAccessRecord, error) {
	return s.record, s.err
}
func (s *signalRepositoryStub) AppendRunSignal(_ context.Context, signal domain.RunSignal, evidence ports.RunSignalEvidence) (domain.RunEvent, error) {
	s.signal, s.evidence = signal, evidence
	return s.event, s.err
}

func TestRunSignalAuthorizesAndAppendsEvidence(t *testing.T) {
	tenant, principal, request, runID, agent, version, owner := applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t)
	now := testTime()
	run := domain.Run{ID: runID, TenantID: tenant, AgentID: agent, AgentVersionID: version, RequestedBy: principal, VersionDigest: "sha256:" + repeat("e", 64), State: domain.RunRunning, StateVersion: 3, EnvelopeVersion: 1, CreatedAt: now, UpdatedAt: now}
	repo := &signalRepositoryStub{record: ports.RunAccessRecord{Run: run, AgentRiskClass: domain.AgentRiskMedium, AgentOwnerID: owner}, event: domain.RunEvent{ID: request, RunID: runID, Sequence: 2}}
	evaluator := &runQueryEvaluatorStub{allow: true}
	service, _ := NewRunSignalService(repo, evaluator, fixedClock{now: now})
	expected := int64(3)
	event, err := service.Signal(context.Background(), SignalRun{TenantID: tenant, PrincipalID: principal, RequestID: request, RunID: runID, Roles: []string{"agent-invoker"}, IdempotencyKey: "abcdefghijklmnop", Type: domain.RunSignalCustom, Payload: []byte(`{"name":"runtime.refresh","data":{}}`), ExpectedStateVersion: &expected})
	if err != nil || event.Sequence != 2 || evaluator.input.Action != "runs.signal" || evaluator.input.Resource.Attributes["signal_type"] != "CUSTOM" || repo.signal.RunID != runID || repo.evidence.PolicyDecisionID.IsZero() {
		t.Fatalf("event=%+v err=%v input=%+v signal=%+v", event, err, evaluator.input, repo.signal)
	}
}

func TestRunSignalDenyAndOutageFailClosed(t *testing.T) {
	tenant, principal, request, runID, agent, version, owner := applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t), applicationID(t)
	now := testTime()
	run := domain.Run{ID: runID, TenantID: tenant, AgentID: agent, AgentVersionID: version, RequestedBy: principal, VersionDigest: "sha256:" + repeat("f", 64), State: domain.RunRunning, StateVersion: 1, EnvelopeVersion: 1, CreatedAt: now, UpdatedAt: now}
	command := SignalRun{TenantID: tenant, PrincipalID: principal, RequestID: request, RunID: runID, IdempotencyKey: "abcdefghijklmnop", Type: domain.RunSignalResume, Payload: []byte(`{}`)}
	for _, tc := range []struct {
		name      string
		evaluator *runQueryEvaluatorStub
		code      domain.ErrorCode
	}{{"deny", &runQueryEvaluatorStub{}, domain.CodeNotFound}, {"outage", &runQueryEvaluatorStub{err: errors.New("opa down")}, domain.CodeUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			service, _ := NewRunSignalService(&signalRepositoryStub{record: ports.RunAccessRecord{Run: run, AgentRiskClass: domain.AgentRiskLow, AgentOwnerID: owner}}, tc.evaluator, fixedClock{now: now})
			_, err := service.Signal(context.Background(), command)
			var typed *domain.Error
			if !errors.As(err, &typed) || typed.Code() != tc.code {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
