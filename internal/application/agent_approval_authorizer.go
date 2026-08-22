package application

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

type PolicyAgentApprovalAuthorizer struct {
	evaluator policy.Evaluator
	now       func() time.Time
}

func NewPolicyAgentApprovalAuthorizer(evaluator policy.Evaluator, now func() time.Time) (*PolicyAgentApprovalAuthorizer, error) {
	if evaluator == nil || now == nil {
		return nil, errors.New("agent approval policy authorizer requires an evaluator and clock")
	}
	return &PolicyAgentApprovalAuthorizer{evaluator: evaluator, now: now}, nil
}

func (authorizer *PolicyAgentApprovalAuthorizer) AuthorizeAgentVersionDecision(ctx context.Context, request AgentVersionDecisionAuthorization) (domain.ID, error) {
	decisionID, err := domain.NewID()
	if err != nil {
		return domain.ID{}, domain.WrapError(domain.CodeInternal, "could not generate policy decision identifier", err)
	}
	roles := append([]string(nil), request.Roles...)
	sort.Strings(roles)
	input := policy.Input{ContractVersion: policy.ContractVersion, DecisionID: decisionID.String(), RequestTime: authorizer.now().UTC(),
		Subject: policy.Subject{PrincipalID: request.ActorPrincipalID.String(), TenantID: request.TenantID.String(), PrincipalType: "human", Roles: roles},
		Action:  "versions.approve", Resource: policy.Resource{Type: "agent_version", ID: request.VersionDigest, TenantID: request.TenantID.String(), Attributes: map[string]any{
			"agent_id": request.AgentID.String(), "decision": strings.ToLower(string(request.Decision))}},
		RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: policy.SecurityState{Authoritative: true}, Context: policy.RequestContext{RequestID: request.RequestID.String()}}
	result, err := authorizer.evaluator.Decide(ctx, input)
	if err != nil {
		return domain.ID{}, domain.WrapError(domain.CodeUnavailable, "authorization policy is unavailable", err).WithRetryable()
	}
	if !result.Decision.Allow {
		return domain.ID{}, domain.NewError(domain.CodeForbidden, "agent version decision is not permitted")
	}
	parsed, err := domain.ParseID(result.Decision.DecisionID)
	if err != nil || parsed != decisionID {
		return domain.ID{}, domain.NewError(domain.CodeUnavailable, "authorization policy returned invalid evidence").WithRetryable()
	}
	return parsed, nil
}
