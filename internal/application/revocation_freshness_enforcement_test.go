package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

type freshnessEvaluatorSpy struct {
	calls int
	input policy.Input
	ttl   int64
	err   error
}

func (s *freshnessEvaluatorSpy) Decide(_ context.Context, in policy.Input) (policy.Result, error) {
	s.calls++
	s.input = in
	return policy.Result{Decision: policy.Decision{ContractVersion: policy.ContractVersion, DecisionID: in.DecisionID, Allow: true, ReasonCodes: []string{"run.access.allowed"}, ResolvedConstraints: map[string]any{}, Obligations: []policy.Obligation{}, DecisionTTLSeconds: s.ttl}}, s.err
}

func freshnessInput(tenant domain.ID, action, risk string) policy.Input {
	return policy.Input{ContractVersion: policy.ContractVersion, DecisionID: "decision", RequestTime: time.Now().UTC(), Subject: policy.Subject{PrincipalID: "principal", TenantID: tenant.String()}, Action: action, Resource: policy.Resource{Type: "run", ID: "run", TenantID: tenant.String(), Attributes: map[string]any{}}, Agent: &policy.Agent{RiskClass: risk}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, Context: policy.RequestContext{RequestID: "request"}, SecurityState: policy.SecurityState{Authoritative: true, GlobalEpoch: 999}}
}

func TestFreshnessEvaluatorEnforcesRiskClassesAndIgnoresCallerState(t *testing.T) {
	tenant := applicationID(t)
	clock := &freshnessTestClock{wall: time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)}
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	epochs := domain.EpochVector{Security: 7, TenantPolicy: 5, TenantRevocation: 4, AgentRevocation: 3}
	if err := tracker.RecordReconciliation(tenant, 9, epochs); err != nil {
		t.Fatal(err)
	}
	clock.advance(29*time.Second+time.Millisecond, 29*time.Second+time.Millisecond)
	spy := &freshnessEvaluatorSpy{ttl: 10}
	evaluator, err := NewFreshnessEnforcingEvaluator(spy, tracker, DefaultRevocationFreshnessPolicy(15*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	result, err := evaluator.Decide(context.Background(), freshnessInput(tenant, "runs.cancel", "medium"))
	if err != nil || !result.Decision.Allow || spy.calls != 1 {
		t.Fatalf("normal write result=%+v calls=%d err=%v", result, spy.calls, err)
	}
	got := spy.input.SecurityState
	if got.Authoritative || got.GlobalEpoch != 7 || got.TenantPolicyEpoch != 5 || got.TenantRevocationEpoch != 4 || got.AgentRevocationEpoch != 3 || got.AgeSeconds != 30 || got.FreshnessMaxAgeSeconds != 30 {
		t.Fatalf("untrusted or imprecise security state reached policy: %+v", got)
	}

	// Sensitive reads retain their separate 60-second availability window.
	clock.advance(20*time.Second, 20*time.Second)
	if result, err = evaluator.Decide(context.Background(), freshnessInput(tenant, "runs.read", "medium")); err != nil || !result.Decision.Allow || spy.input.SecurityState.FreshnessMaxAgeSeconds != 60 {
		t.Fatalf("sensitive read result=%+v state=%+v err=%v", result, spy.input.SecurityState, err)
	}
	// The explicitly tighter low-risk bound has already expired.
	before := spy.calls
	result, err = evaluator.Decide(context.Background(), freshnessInput(tenant, "agents.list", "low"))
	if err != nil || result.Decision.Allow || result.Decision.ReasonCodes[0] != "security_state.stale" || spy.calls != before {
		t.Fatalf("low-risk stale result=%+v calls=%d err=%v", result, spy.calls, err)
	}
}

func TestFreshnessEvaluatorFailsClosedWithoutStateOrHealthyClock(t *testing.T) {
	tenant := applicationID(t)
	clock := &freshnessTestClock{wall: time.Now().UTC()}
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	spy := &freshnessEvaluatorSpy{}
	evaluator, _ := NewFreshnessEnforcingEvaluator(spy, tracker, DefaultRevocationFreshnessPolicy(time.Minute))

	got, err := evaluator.Decide(context.Background(), freshnessInput(tenant, "runs.read", "low"))
	if err != nil || got.Decision.Allow || spy.calls != 0 {
		t.Fatalf("unknown state did not fail closed: result=%+v calls=%d err=%v", got, spy.calls, err)
	}
	if err = tracker.RecordStreamReceipt(tenant, 1, domain.EpochVector{Security: 1}); err != nil {
		t.Fatal(err)
	}
	clock.advance(0, -time.Second)
	got, err = evaluator.Decide(context.Background(), freshnessInput(tenant, "runs.read", "low"))
	if err != nil || got.Decision.Allow || spy.calls != 0 {
		t.Fatalf("clock regression did not fail closed: result=%+v calls=%d err=%v", got, spy.calls, err)
	}
}

func TestFreshnessEvaluatorFailsClosedDuringLagGapAndPartitionThenRecovers(t *testing.T) {
	tenant := applicationID(t)
	clock := &freshnessTestClock{wall: time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)}
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	spy := &freshnessEvaluatorSpy{ttl: 10}
	evaluator, _ := NewFreshnessEnforcingEvaluator(spy, tracker, DefaultRevocationFreshnessPolicy(15*time.Second))
	input := freshnessInput(tenant, "runs.cancel", "medium")

	if err := tracker.RecordReconciliation(tenant, 4, domain.EpochVector{Security: 4}); err != nil {
		t.Fatal(err)
	}
	if got, err := evaluator.Decide(context.Background(), input); err != nil || !got.Decision.Allow {
		t.Fatalf("initial decision=%+v err=%v", got, err)
	}
	if err := tracker.RecordAuthoritativeHead(tenant, 5); err != nil {
		t.Fatal(err)
	}
	if got, err := evaluator.Decide(context.Background(), input); err != nil || got.Decision.Allow || spy.calls != 1 {
		t.Fatalf("lag did not fail closed: decision=%+v calls=%d err=%v", got, spy.calls, err)
	}
	if err := tracker.RecordGap(tenant, 6); err != nil {
		t.Fatal(err)
	}
	if got, err := evaluator.Decide(context.Background(), input); err != nil || got.Decision.Allow || spy.calls != 1 {
		t.Fatalf("gap did not fail closed: decision=%+v calls=%d err=%v", got, spy.calls, err)
	}
	if err := tracker.RecordReconciliation(tenant, 6, domain.EpochVector{Security: 6}); err != nil {
		t.Fatal(err)
	}
	if got, err := evaluator.Decide(context.Background(), input); err != nil || !got.Decision.Allow || spy.calls != 2 || spy.input.SecurityState.GlobalEpoch != 6 {
		t.Fatalf("reconciliation recovery=%+v calls=%d state=%+v err=%v", got, spy.calls, spy.input.SecurityState, err)
	}
	clock.advance(31*time.Second, 31*time.Second)
	if got, err := evaluator.Decide(context.Background(), input); err != nil || got.Decision.Allow || spy.calls != 2 {
		t.Fatalf("partition did not expire normal-write trust: decision=%+v calls=%d err=%v", got, spy.calls, err)
	}
}

func TestFreshnessEvaluatorRequiresLiveZeroTTLForHighRiskWrites(t *testing.T) {
	tenant := applicationID(t)
	clock := &freshnessTestClock{wall: time.Now().UTC()}
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	spy := &freshnessEvaluatorSpy{}
	evaluator, _ := NewFreshnessEnforcingEvaluator(spy, tracker, DefaultRevocationFreshnessPolicy(time.Minute))

	got, err := evaluator.Decide(context.Background(), freshnessInput(tenant, "resources.settle", "low"))
	if err != nil || !got.Decision.Allow || !spy.input.SecurityState.Authoritative || spy.input.SecurityState.GlobalEpoch != 0 {
		t.Fatalf("high-risk write was not live: result=%+v state=%+v err=%v", got, spy.input.SecurityState, err)
	}
	spy.ttl = 1
	_, err = evaluator.Decide(context.Background(), freshnessInput(tenant, "runs.create", "critical"))
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code() != domain.CodeUnavailable {
		t.Fatalf("cacheable high-risk ALLOW error=%v", err)
	}
}

func TestFreshnessPolicyRejectsLoosenedOrUnboundedConfiguration(t *testing.T) {
	tracker, _ := NewRevocationFreshnessTracker(fixedClock{now: time.Now().UTC()})
	spy := &freshnessEvaluatorSpy{}
	bad := []RevocationFreshnessPolicy{
		{},
		{NormalWriteMaxAge: 31 * time.Second, SensitiveReadMaxAge: time.Minute, LowRiskReadMaxAge: time.Second},
		{NormalWriteMaxAge: 30 * time.Second, SensitiveReadMaxAge: 61 * time.Second, LowRiskReadMaxAge: time.Second},
		{NormalWriteMaxAge: 30 * time.Second, SensitiveReadMaxAge: time.Minute},
	}
	for _, bounds := range bad {
		if _, err := NewFreshnessEnforcingEvaluator(spy, tracker, bounds); err == nil {
			t.Fatalf("accepted unsafe bounds: %+v", bounds)
		}
	}
}

func TestClassifyFreshnessConservatively(t *testing.T) {
	tenant := applicationID(t)
	tests := []struct {
		action, risk string
		want         FreshnessClass
	}{
		{"agents.list", "low", FreshnessLowRiskRead}, {"runs.read", "low", FreshnessSensitiveRead},
		{"runs.cancel", "critical", FreshnessNormalWrite}, {"runs.create", "medium", FreshnessNormalWrite},
		{"runs.create", "high", FreshnessHighRiskWrite}, {"resources.meter", "low", FreshnessHighRiskWrite},
		{"future.action", "low", FreshnessHighRiskWrite},
	}
	for _, tt := range tests {
		if got := ClassifyFreshness(freshnessInput(tenant, tt.action, tt.risk)); got != tt.want {
			t.Errorf("%s/%s classified %s, want %s", tt.action, tt.risk, got, tt.want)
		}
	}
}
