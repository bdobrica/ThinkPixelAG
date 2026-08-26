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
	calls  int
}

func (r *usageRepoStub) GetRun(context.Context, domain.ID) (ports.RunAccessRecord, error) {
	return r.record, nil
}
func (r *usageRepoStub) RecordTrustedUsage(_ context.Context, u domain.TrustedUsage) (domain.UsageReceipt, error) {
	r.calls++
	r.usage = u
	return domain.UsageReceipt{UsageID: u.ID, AcceptedAt: u.RecordedAt}, nil
}

type usagePolicyStub struct {
	input policy.Input
	allow bool
	err   error
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
