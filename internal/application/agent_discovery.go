package application

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const discoveryScanSize = 200

type AgentDiscovery struct {
	repository ports.AgentDiscoveryRepository
	evaluator  policy.Evaluator
	clock      domain.Clock
}

func NewAgentDiscovery(repository ports.AgentDiscoveryRepository, evaluator policy.Evaluator, clock domain.Clock) (*AgentDiscovery, error) {
	if repository == nil || evaluator == nil || clock == nil {
		return nil, errors.New("agent discovery requires a repository, policy evaluator, and clock")
	}
	return &AgentDiscovery{repository: repository, evaluator: evaluator, clock: clock}, nil
}

type DiscoverAgents struct {
	TenantID, PrincipalID, RequestID domain.ID
	Roles                            []string
	Issuer                           string
	After                            domain.ID
	Limit                            int
	SecurityState                    policy.SecurityState
}

type AgentPage struct {
	Items []domain.Agent
	Next  domain.ID
}

func (service *AgentDiscovery) List(ctx context.Context, command DiscoverAgents) (AgentPage, error) {
	if err := validateDiscovery(command.TenantID, command.PrincipalID, command.RequestID, command.Limit); err != nil {
		return AgentPage{}, err
	}
	if allowed, err := service.authorize(ctx, command, "agents.list", nil); err != nil {
		return AgentPage{}, err
	} else if !allowed {
		return AgentPage{}, domain.NewError(domain.CodeForbidden, "agent discovery is not permitted")
	}

	page := AgentPage{Items: make([]domain.Agent, 0, command.Limit)}
	after := command.After
	for len(page.Items) <= command.Limit {
		agents, err := service.repository.ListAgents(ctx, ports.AgentListQuery{After: after, Limit: discoveryScanSize})
		if err != nil {
			return AgentPage{}, err
		}
		if len(agents) == 0 {
			break
		}
		for _, agent := range agents {
			if agent.TenantID != command.TenantID || (!after.IsZero() && agent.ID.String() <= after.String()) {
				return AgentPage{}, domain.NewError(domain.CodeInternal, "agent discovery repository crossed its authority boundary")
			}
			after = agent.ID
			visible, err := service.visible(ctx, command, agent)
			if err != nil {
				return AgentPage{}, err
			}
			if visible {
				page.Items = append(page.Items, agent)
				if len(page.Items) > command.Limit {
					page.Next = page.Items[command.Limit-1].ID
					page.Items = page.Items[:command.Limit]
					return page, nil
				}
			}
		}
		if len(agents) < discoveryScanSize {
			break
		}
	}
	return page, nil
}

func (service *AgentDiscovery) Describe(ctx context.Context, command DiscoverAgents, agentID domain.ID) (domain.Agent, error) {
	if err := validateDiscovery(command.TenantID, command.PrincipalID, command.RequestID, 1); err != nil || agentID.IsZero() {
		if err != nil {
			return domain.Agent{}, err
		}
		return domain.Agent{}, domain.NewError(domain.CodeInvalidArgument, "agent identifier is invalid")
	}
	agent, err := service.repository.DescribeAgent(ctx, agentID)
	if err != nil {
		return domain.Agent{}, enumerationSafe(err)
	}
	if agent.TenantID != command.TenantID {
		return domain.Agent{}, domain.NewError(domain.CodeNotFound, "agent not found")
	}
	visible, err := service.visible(ctx, command, agent)
	if err != nil {
		return domain.Agent{}, err
	}
	if !visible {
		return domain.Agent{}, domain.NewError(domain.CodeNotFound, "agent not found")
	}
	return agent, nil
}

func validateDiscovery(tenantID, principalID, requestID domain.ID, limit int) error {
	if tenantID.IsZero() || principalID.IsZero() || requestID.IsZero() || limit < 1 || limit > 200 {
		return domain.NewError(domain.CodeInvalidArgument, "agent discovery request is invalid")
	}
	return nil
}

func (service *AgentDiscovery) visible(ctx context.Context, command DiscoverAgents, agent domain.Agent) (bool, error) {
	if agent.Status != domain.AgentActive {
		return false, nil
	}
	candidates, err := service.repository.ListAgentVersionCandidates(ctx, agent.ID, "")
	if err != nil {
		return false, enumerationSafe(err)
	}
	if len(candidates) == 0 {
		return false, nil
	}
	candidate := candidates[0]
	if candidate.Agent != agent || candidate.Agent.TenantID != command.TenantID || candidate.Version.AgentID != agent.ID || candidate.Version.TenantID != command.TenantID || candidate.Approval.TenantID != command.TenantID || candidate.Approval.AgentID != agent.ID || candidate.Approval.AgentVersionID != candidate.Version.ID || candidate.State != domain.AgentVersionApproved || candidate.Approval.Decision != domain.DecisionApprove {
		return false, domain.NewError(domain.CodeInternal, "agent discovery candidate crossed its authority boundary")
	}
	return service.authorize(ctx, command, "agents.describe", &candidate)
}

func (service *AgentDiscovery) authorize(ctx context.Context, command DiscoverAgents, action string, candidate *domain.AgentVersionCandidate) (bool, error) {
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return false, domain.WrapError(domain.CodeInternal, "agent discovery clock is invalid", err)
	}
	decisionID, err := domain.NewID()
	if err != nil {
		return false, domain.WrapError(domain.CodeInternal, "could not generate discovery decision identifier", err)
	}
	roles := append([]string(nil), command.Roles...)
	sort.Strings(roles)
	input := policy.Input{ContractVersion: policy.ContractVersion, DecisionID: decisionID.String(), RequestTime: now,
		Subject: policy.Subject{PrincipalID: command.PrincipalID.String(), TenantID: command.TenantID.String(), PrincipalType: "human", Roles: roles, Issuer: command.Issuer},
		Action:  action, Resource: policy.Resource{Type: "agent_catalog", TenantID: command.TenantID.String(), Attributes: map[string]any{}},
		RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: command.SecurityState, Context: policy.RequestContext{RequestID: command.RequestID.String()}}
	if candidate != nil {
		input.Resource.Type, input.Resource.ID = "agent", candidate.Agent.ID.String()
		input.Agent = &policy.Agent{ID: candidate.Agent.ID.String(), VersionDigest: candidate.Version.ContentDigest, RiskClass: strings.ToLower(string(candidate.Agent.RiskClass)), Owner: candidate.Agent.OwnerPrincipalID.String(), Approved: true}
	}
	result, err := service.evaluator.Decide(ctx, input)
	if err != nil {
		return false, domain.WrapError(domain.CodeUnavailable, "agent discovery policy is unavailable", err).WithRetryable()
	}
	if result.Decision.DecisionID != decisionID.String() {
		return false, domain.NewError(domain.CodeUnavailable, "agent discovery policy returned invalid evidence").WithRetryable()
	}
	return result.Decision.Allow, nil
}

func enumerationSafe(err error) error {
	var typed *domain.Error
	if errors.As(err, &typed) && typed.Code() == domain.CodeNotFound {
		return domain.NewError(domain.CodeNotFound, "agent not found")
	}
	return err
}
