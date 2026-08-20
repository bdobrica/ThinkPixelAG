package application

import (
	"context"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }

type agentRepositoryStub struct {
	agent       domain.Agent
	eligibility map[domain.ID]ports.PrincipalEligibility
	created     bool
	updated     bool
	list        []domain.Agent
}

func (stub *agentRepositoryStub) PrincipalEligibility(_ context.Context, id domain.ID) (ports.PrincipalEligibility, error) {
	return stub.eligibility[id], nil
}
func (stub *agentRepositoryStub) CreateAgent(_ context.Context, agent domain.Agent) error {
	stub.agent, stub.created = agent, true
	return nil
}
func (stub *agentRepositoryStub) UpdateAgent(_ context.Context, agent domain.Agent, _ time.Time) error {
	stub.agent, stub.updated = agent, true
	return nil
}
func (stub *agentRepositoryStub) DescribeAgent(context.Context, domain.ID) (domain.Agent, error) {
	return stub.agent, nil
}
func (stub *agentRepositoryStub) ListAgents(context.Context, ports.AgentListQuery) ([]domain.Agent, error) {
	return stub.list, nil
}

func TestAgentRegistryCreateUpdateListDescribe(t *testing.T) {
	t.Parallel()
	tenant, owner, sponsor, agentID := applicationID(t), applicationID(t), applicationID(t), applicationID(t)
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	stub := &agentRepositoryStub{eligibility: map[domain.ID]ports.PrincipalEligibility{owner: {Exists: true}, sponsor: {Exists: true}}}
	service, err := NewAgentRegistry(stub, fixedClock{now})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := service.Create(context.Background(), CreateAgent{ID: agentID, TenantID: tenant, OwnerPrincipalID: owner, SponsorPrincipalID: sponsor, Name: "reviewer", Description: "Reviews payments", RiskClass: domain.AgentRiskHigh})
	if err != nil || !stub.created || agent.Status != domain.AgentActive {
		t.Fatalf("Create = %+v, %v", agent, err)
	}
	service.clock = fixedClock{now.Add(time.Second)}
	updated, err := service.Update(context.Background(), UpdateAgent{TenantID: tenant, AgentID: agentID, OwnerPrincipalID: owner, SponsorPrincipalID: sponsor, Name: "reviewer-v2", Description: "Reviews payments", RiskClass: domain.AgentRiskCritical, Status: domain.AgentSuspended, ExpectedUpdatedAt: now})
	if err != nil || !stub.updated || updated.Status != domain.AgentSuspended {
		t.Fatalf("Update = %+v, %v", updated, err)
	}
	stub.list = []domain.Agent{updated}
	listed, err := service.List(context.Background(), tenant, ports.AgentListQuery{Limit: 20})
	if err != nil || len(listed) != 1 {
		t.Fatalf("List = %+v, %v", listed, err)
	}
	described, err := service.Describe(context.Background(), tenant, agentID)
	if err != nil || described.ID != agentID {
		t.Fatalf("Describe = %+v, %v", described, err)
	}
}

func TestAgentRegistryRejectsInvalidPrincipalsConcurrencyAndTenantLeak(t *testing.T) {
	t.Parallel()
	tenant, owner, sponsor, agentID := applicationID(t), applicationID(t), applicationID(t), applicationID(t)
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	base := domain.Agent{ID: agentID, TenantID: tenant, Name: "reviewer", OwnerPrincipalID: owner, SponsorPrincipalID: sponsor, RiskClass: domain.AgentRiskHigh, Status: domain.AgentActive, CreatedAt: now, UpdatedAt: now}
	stub := &agentRepositoryStub{agent: base, eligibility: map[domain.ID]ports.PrincipalEligibility{owner: {Exists: true}, sponsor: {Exists: true, Disabled: true}}}
	service, _ := NewAgentRegistry(stub, fixedClock{now.Add(time.Second)})
	_, err := service.Create(context.Background(), CreateAgent{ID: agentID, TenantID: tenant, OwnerPrincipalID: owner, SponsorPrincipalID: sponsor, Name: "reviewer", RiskClass: domain.AgentRiskHigh})
	if domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("disabled sponsor error = %v", err)
	}
	_, err = service.Update(context.Background(), UpdateAgent{TenantID: tenant, AgentID: agentID, OwnerPrincipalID: owner, SponsorPrincipalID: sponsor, Name: "reviewer", RiskClass: domain.AgentRiskHigh, Status: domain.AgentActive, ExpectedUpdatedAt: now.Add(-time.Second)})
	if domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("stale update error = %v", err)
	}
	otherTenant := applicationID(t)
	_, err = service.Describe(context.Background(), otherTenant, agentID)
	if domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-tenant describe error = %v", err)
	}
	stub.list = []domain.Agent{base}
	_, err = service.List(context.Background(), otherTenant, ports.AgentListQuery{Limit: 20})
	if domain.ErrorCodeOf(err) != domain.CodeInternal {
		t.Fatalf("cross-tenant list error = %v", err)
	}
}

func applicationID(t *testing.T) domain.ID {
	t.Helper()
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
