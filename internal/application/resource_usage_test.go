package application

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type usageRepoStub struct {
	record ports.RunAccessRecord
	usage  domain.TrustedUsage
	hint   domain.ThroughputHint
	err    error
	calls  int
}

func (r *usageRepoStub) GetRun(context.Context, domain.ID) (ports.RunAccessRecord, error) {
	return r.record, nil
}
func (r *usageRepoStub) RecordTrustedUsage(_ context.Context, u domain.TrustedUsage, hint domain.ThroughputHint) (domain.UsageReceipt, error) {
	r.calls++
	r.usage = u
	r.hint = hint
	return domain.UsageReceipt{UsageID: u.ID, AcceptedAt: u.RecordedAt}, r.err
}

type usageRateStub struct {
	blocked, marked bool
	err             error
}

func (a *usageRateStub) Blocked(context.Context, string) (bool, error) { return a.blocked, a.err }
func (a *usageRateStub) MarkBlocked(context.Context, string, time.Time) error {
	a.marked = true
	return a.err
}

type usagePolicyStub struct {
	input policy.Input
	allow bool
	err   error
}

func TestTrustedUsageRateAcceleratorIsConservativeAndOptional(t *testing.T) {
	ids := make([]domain.ID, 7)
	for i := range ids {
		ids[i], _ = domain.NewID()
	}
	now := time.Date(2026, 8, 26, 12, 34, 0, 0, time.UTC)
	repo := &usageRepoStub{record: ports.RunAccessRecord{Run: domain.Run{ID: ids[0], TenantID: ids[1], AgentID: ids[2], AgentVersionID: ids[3], RequestedBy: ids[4], VersionDigest: "sha256:" + strings.Repeat("a", 64), State: domain.RunRunning, StateVersion: 2, EnvelopeVersion: 1, CreatedAt: now, UpdatedAt: now}, AgentRiskClass: domain.AgentRiskLow, AgentOwnerID: ids[5]}}
	accelerator := &usageRateStub{blocked: true}
	service, _ := NewTrustedUsageService(repo, &usagePolicyStub{allow: true}, fixedClock{now: now}, accelerator)
	command := RecordTrustedUsage{TenantID: ids[1], ProducerID: ids[6], RequestID: ids[4], RunID: ids[0], SourceEventID: "evt-rate", ResourceName: "tool_calls", Unit: "calls", Quantity: 1, ObservedAt: now, SecurityState: policy.SecurityState{Authoritative: true}}
	if _, err := service.Record(context.Background(), command); err != nil || !repo.hint.Blocked || repo.hint.DimensionName != "tool_calls_per_minute" {
		t.Fatalf("hint=%+v error=%v", repo.hint, err)
	}
	accelerator.err = context.DeadlineExceeded
	if _, err := service.Record(context.Background(), command); err != nil {
		t.Fatalf("accelerator outage must bypass to repository: %v", err)
	}
	repo.err = &domain.ThroughputLimitExceeded{DimensionName: "tool_calls_per_minute", RetryAt: now.Add(time.Minute)}
	accelerator.err = nil
	if _, err := service.Record(context.Background(), command); domain.ErrorCodeOf(err) != domain.CodeConflict || !accelerator.marked {
		t.Fatalf("rate denial=%v marked=%v", err, accelerator.marked)
	}
}

func (p *usagePolicyStub) Decide(_ context.Context, in policy.Input) (policy.Result, error) {
	p.input = in
	return policy.Result{Decision: policy.Decision{DecisionID: in.DecisionID, Allow: p.allow, ReasonCodes: []string{"workload.operation.allowed"}, ResolvedConstraints: map[string]any{}, Obligations: []policy.Obligation{}}}, p.err
}

func TestTrustedUsageAuthenticatesAuthorizesAndPersistsProducer(t *testing.T) {
	ids := make([]domain.ID, 7)
	for i := range ids {
		ids[i], _ = domain.NewID()
	}
	now := time.Unix(20, 0).UTC()
	repo := &usageRepoStub{record: ports.RunAccessRecord{Run: domain.Run{ID: ids[0], TenantID: ids[1], AgentID: ids[2], AgentVersionID: ids[3], RequestedBy: ids[4], VersionDigest: "sha256:" + strings.Repeat("a", 64), State: domain.RunRunning, StateVersion: 2, EnvelopeVersion: 1, CreatedAt: now, UpdatedAt: now}, AgentRiskClass: domain.AgentRiskLow, AgentOwnerID: ids[5]}}
	eval := &usagePolicyStub{allow: true}
	service, _ := NewTrustedUsageService(repo, eval, fixedClock{now: now})
	_, err := service.Record(context.Background(), RecordTrustedUsage{TenantID: ids[1], ProducerID: ids[6], RequestID: ids[4], RunID: ids[0], Roles: []string{"trusted-meter"}, SourceEventID: "evt-1", ResourceName: "llm_tokens", Unit: "llm_tokens", Quantity: 2, ObservedAt: now, SecurityState: policy.SecurityState{Authoritative: true}})
	if err != nil || repo.calls != 1 || repo.usage.ProducerID != ids[6] || eval.input.Action != "resources.meter" || eval.input.Subject.PrincipalType != "workload" {
		t.Fatalf("calls=%d input=%+v err=%v", repo.calls, eval.input, err)
	}
	eval.allow = false
	if _, err = service.Record(context.Background(), RecordTrustedUsage{TenantID: ids[1], ProducerID: ids[6], RequestID: ids[4], RunID: ids[0], SourceEventID: "evt-2", ResourceName: "llm_tokens", Unit: "llm_tokens", Quantity: 1, ObservedAt: now}); domain.ErrorCodeOf(err) != domain.CodeForbidden || repo.calls != 1 {
		t.Fatalf("denial err=%v calls=%d", err, repo.calls)
	}
}
