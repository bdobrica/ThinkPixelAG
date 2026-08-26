package application

import (
	"context"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type settlementRepoStub struct {
	target     ports.ResourceSettlementTarget
	settlement domain.ResourceSettlement
	evidence   ports.ResourceSettlementEvidence
}

func (s *settlementRepoStub) GetSettlementTarget(context.Context, domain.ID) (ports.ResourceSettlementTarget, error) {
	return s.target, nil
}
func (s *settlementRepoStub) SettleReservation(_ context.Context, v domain.ResourceSettlement, e ports.ResourceSettlementEvidence) (domain.ResourceSettlementResult, error) {
	s.settlement = v
	s.evidence = e
	return domain.ResourceSettlementResult{ID: v.ID, ReservationID: v.ReservationID, SettledAt: v.SettledAt}, nil
}

type settlementPolicyStub struct{ input policy.Input }

func (s *settlementPolicyStub) Decide(_ context.Context, in policy.Input) (policy.Result, error) {
	s.input = in
	return policy.Result{Decision: policy.Decision{DecisionID: in.DecisionID, Allow: true, ReasonCodes: []string{"workload.operation.allowed"}}}, nil
}

func TestResourceSettlementAuthorizesTrustedTerminalAction(t *testing.T) {
	ids := make([]domain.ID, 8)
	for i := range ids {
		ids[i], _ = domain.NewID()
	}
	now := time.Unix(30, 0).UTC()
	run := domain.Run{ID: ids[0], TenantID: ids[1], AgentID: ids[2], AgentVersionID: ids[3], RequestedBy: ids[4], VersionDigest: "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", State: domain.RunCompleted, StateVersion: 3, EnvelopeVersion: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	repo := &settlementRepoStub{target: ports.ResourceSettlementTarget{Run: ports.RunAccessRecord{Run: run, AgentRiskClass: domain.AgentRiskLow, AgentOwnerID: ids[4]}, ChildRunID: ids[0]}}
	eval := &settlementPolicyStub{}
	service, _ := NewResourceSettlementService(repo, eval, fixedClock{now: now})
	_, err := service.Settle(context.Background(), SettleReservation{TenantID: ids[1], PrincipalID: ids[5], RequestID: ids[6], ReservationID: ids[7], Roles: []string{"trusted-workload"}, IdempotencyKey: "settle-1", TerminalRunState: "COMPLETED", SecurityState: policy.SecurityState{Authoritative: true}})
	if err != nil {
		t.Fatal(err)
	}
	if eval.input.Action != "resources.settle" || eval.input.Resource.ID != ids[7].String() || repo.settlement.PolicyDecisionID.IsZero() || repo.evidence.AuditID.IsZero() {
		t.Fatalf("input=%+v settlement=%+v evidence=%+v", eval.input, repo.settlement, repo.evidence)
	}
}
