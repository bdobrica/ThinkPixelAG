package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type AgentRegistry struct {
	repository ports.AgentRegistry
	clock      domain.Clock
}

func NewAgentRegistry(repository ports.AgentRegistry, clock domain.Clock) (*AgentRegistry, error) {
	if repository == nil || clock == nil {
		return nil, errors.New("agent registry requires a repository and clock")
	}
	return &AgentRegistry{repository: repository, clock: clock}, nil
}

type CreateAgent struct {
	ID, TenantID, OwnerPrincipalID, SponsorPrincipalID domain.ID
	Name, Description                                  string
	RiskClass                                          domain.AgentRiskClass
}

func (service *AgentRegistry) Create(ctx context.Context, command CreateAgent) (domain.Agent, error) {
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.Agent{}, domain.WrapError(domain.CodeInternal, "agent registry clock is invalid", err)
	}
	agent := domain.Agent{ID: command.ID, TenantID: command.TenantID, Name: command.Name, Description: command.Description, OwnerPrincipalID: command.OwnerPrincipalID, SponsorPrincipalID: command.SponsorPrincipalID, RiskClass: command.RiskClass, Status: domain.AgentActive, CreatedAt: now, UpdatedAt: now}
	if err := agent.Validate(); err != nil {
		return domain.Agent{}, domain.WrapError(domain.CodeInvalidArgument, "agent details are invalid", err)
	}
	if err := service.validatePrincipals(ctx, agent); err != nil {
		return domain.Agent{}, err
	}
	if err := service.repository.CreateAgent(ctx, agent); err != nil {
		return domain.Agent{}, err
	}
	return agent, nil
}

type UpdateAgent struct {
	TenantID, AgentID, OwnerPrincipalID, SponsorPrincipalID domain.ID
	Name, Description                                       string
	RiskClass                                               domain.AgentRiskClass
	Status                                                  domain.AgentStatus
	ExpectedUpdatedAt                                       time.Time
}

func (service *AgentRegistry) Update(ctx context.Context, command UpdateAgent) (domain.Agent, error) {
	current, err := service.repository.DescribeAgent(ctx, command.AgentID)
	if err != nil {
		return domain.Agent{}, err
	}
	if current.TenantID != command.TenantID {
		return domain.Agent{}, domain.NewError(domain.CodeNotFound, "agent not found")
	}
	if command.ExpectedUpdatedAt.IsZero() || !command.ExpectedUpdatedAt.Equal(current.UpdatedAt) {
		return domain.Agent{}, domain.NewError(domain.CodeConflict, "agent was modified")
	}
	if !current.CanTransitionTo(command.Status) {
		return domain.Agent{}, domain.NewError(domain.CodeConflict, "agent state transition is not allowed")
	}
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.Agent{}, domain.WrapError(domain.CodeInternal, "agent registry clock is invalid", err)
	}
	if !now.After(current.UpdatedAt) {
		return domain.Agent{}, domain.NewError(domain.CodeInternal, "agent registry clock did not advance")
	}
	updated := current
	updated.Name, updated.Description = command.Name, command.Description
	updated.OwnerPrincipalID, updated.SponsorPrincipalID = command.OwnerPrincipalID, command.SponsorPrincipalID
	updated.RiskClass, updated.Status, updated.UpdatedAt = command.RiskClass, command.Status, now
	if err := updated.Validate(); err != nil {
		return domain.Agent{}, domain.WrapError(domain.CodeInvalidArgument, "agent details are invalid", err)
	}
	if err := service.validatePrincipals(ctx, updated); err != nil {
		return domain.Agent{}, err
	}
	if err := service.repository.UpdateAgent(ctx, updated, command.ExpectedUpdatedAt); err != nil {
		return domain.Agent{}, err
	}
	return updated, nil
}

func (service *AgentRegistry) Describe(ctx context.Context, tenantID, agentID domain.ID) (domain.Agent, error) {
	if tenantID.IsZero() || agentID.IsZero() {
		return domain.Agent{}, domain.NewError(domain.CodeInvalidArgument, "tenant and agent identifiers are required")
	}
	agent, err := service.repository.DescribeAgent(ctx, agentID)
	if err != nil {
		return domain.Agent{}, err
	}
	if agent.TenantID != tenantID {
		return domain.Agent{}, domain.NewError(domain.CodeNotFound, "agent not found")
	}
	return agent, nil
}

func (service *AgentRegistry) List(ctx context.Context, tenantID domain.ID, query ports.AgentListQuery) ([]domain.Agent, error) {
	if tenantID.IsZero() || query.Limit < 1 || query.Limit > 200 {
		return nil, domain.NewError(domain.CodeInvalidArgument, "tenant and a list limit from 1 to 200 are required")
	}
	agents, err := service.repository.ListAgents(ctx, query)
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		if agent.TenantID != tenantID {
			return nil, domain.NewError(domain.CodeInternal, "agent repository returned an invalid tenant scope")
		}
	}
	return agents, nil
}

func (service *AgentRegistry) validatePrincipals(ctx context.Context, agent domain.Agent) error {
	for _, principal := range []struct {
		label string
		id    domain.ID
	}{{"owner", agent.OwnerPrincipalID}, {"sponsor", agent.SponsorPrincipalID}} {
		eligibility, err := service.repository.PrincipalEligibility(ctx, principal.id)
		if err != nil {
			return err
		}
		if !eligibility.Exists || eligibility.Disabled {
			return domain.NewError(domain.CodeInvalidArgument, fmt.Sprintf("agent %s is not an active tenant principal", principal.label))
		}
	}
	return nil
}
