package application

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

func TestAdmissionInheritsApprovedDeploymentAndPolicyLimits(t *testing.T) {
	for _, tc := range []struct {
		name       string
		request    map[string]any
		wantTokens string
	}{
		{"omitted", nil, "80"},
		{"partial stricter", map[string]any{"max_llm_tokens": json.Number("20")}, "20"},
		{"larger request", map[string]any{"max_llm_tokens": json.Number("200")}, "80"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := resolutionCandidate(t, domain.AgentVersionApproved, "a")
			tokens, deadline, rate := int64(80), int64(120), int64(5)
			candidate.Version.Manifest.Limits = domain.AgentLimits{MaxLLMTokens: &tokens, MaxExecutionTimeSeconds: &deadline, MaxToolCallsPerMinute: &rate}
			repository := &admissionRepositoryStub{}
			evaluator := policyEvaluatorFunc(func(_ context.Context, in policy.Input) (policy.Result, error) {
				if fmt.Sprint(in.AuthorityConstraints["max_llm_tokens"]) != "80" || fmt.Sprint(in.AuthorityConstraints["max_execution_time_seconds"]) != "120" {
					t.Fatalf("policy did not receive approved bounds: %v", in.AuthorityConstraints)
				}
				return policy.Result{Decision: policy.Decision{Allow: true, DecisionID: in.DecisionID, ResolvedConstraints: map[string]any{"max_tool_calls": 3}}, Metadata: policy.Metadata{PolicyDigest: candidate.Version.ContentDigest, PolicyVersion: 1}}, nil
			})
			resolver, _ := NewVersionResolver(&resolutionRepositoryStub{candidates: []domain.AgentVersionCandidate{candidate}}, evaluator, fixedClock{now: testTime()})
			service, _ := NewRunAdmissionService(resolver, repository, fixedClock{now: testTime()})
			command := AdmitRun{TenantID: candidate.Agent.TenantID, PrincipalID: applicationID(t), AgentID: candidate.Agent.ID, RequestID: applicationID(t), RequestedConstraints: tc.request, AuthorityConstraints: map[string]any{"max_llm_tokens": 100, "max_execution_time_seconds": 300, "max_tool_calls": 10}}
			got, err := service.Admit(context.Background(), command)
			if err != nil {
				t.Fatal(err)
			}
			if got.DeadlineAt == nil || got.DeadlineAt.Sub(got.CreatedAt).Seconds() != 120 || fmt.Sprint(got.Constraints["max_llm_tokens"]) != tc.wantTokens || got.Constraints["max_tool_calls"] != 3 || fmt.Sprint(got.Constraints["max_tool_calls_per_minute"]) != "5" {
				t.Fatalf("admission=%+v", got)
			}
			grants, err := domain.RootResourceGrants(got.Constraints)
			if err != nil || len(grants) != 3 || repository.calls != 1 {
				t.Fatalf("grants=%v persistence calls=%d err=%v", grants, repository.calls, err)
			}
		})
	}
}

func TestAdmissionRejectsMissingAuthorityAndMalformedPolicyBeforePersistence(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		authority, request, output map[string]any
	}{
		{"no authority", map[string]any{}, nil, map[string]any{}},
		{"unbacked request", map[string]any{"max_execution_time_seconds": 60}, map[string]any{"max_llm_tokens": 10}, map[string]any{}},
		{"fractional grant", map[string]any{"max_llm_tokens": 100}, nil, map[string]any{"max_llm_tokens": 1.5}},
		{"invalid deadline", map[string]any{"max_execution_time_seconds": 60}, nil, map[string]any{"max_execution_time_seconds": 0}},
		{"expanded policy", map[string]any{"max_llm_tokens": 100}, nil, map[string]any{"max_llm_tokens": 101}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := resolutionCandidate(t, domain.AgentVersionApproved, "a")
			repository := &admissionRepositoryStub{}
			evaluator := policyEvaluatorFunc(func(_ context.Context, in policy.Input) (policy.Result, error) {
				return policy.Result{Decision: policy.Decision{Allow: true, DecisionID: in.DecisionID, ResolvedConstraints: tc.output}, Metadata: policy.Metadata{PolicyDigest: candidate.Version.ContentDigest, PolicyVersion: 1}}, nil
			})
			resolver, _ := NewVersionResolver(&resolutionRepositoryStub{candidates: []domain.AgentVersionCandidate{candidate}}, evaluator, fixedClock{now: testTime()})
			service, _ := NewRunAdmissionService(resolver, repository, fixedClock{now: testTime()})
			_, err := service.Admit(context.Background(), AdmitRun{TenantID: candidate.Agent.TenantID, PrincipalID: applicationID(t), AgentID: candidate.Agent.ID, RequestID: applicationID(t), RequestedConstraints: tc.request, AuthorityConstraints: tc.authority})
			if err == nil || repository.calls != 0 {
				t.Fatalf("err=%v persisted=%d", err, repository.calls)
			}
		})
	}
}
