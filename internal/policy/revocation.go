package policy

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

// RevocationEvaluator establishes authoritative epochs and active revocations
// before every policy decision. Non-authoritative callers must present an
// exact applicable epoch vector; mismatches fail closed before cached policy.
type RevocationEvaluator struct {
	next      Evaluator
	authority ports.RevocationAuthority
	now       func() time.Time
}

func NewRevocationEvaluator(next Evaluator, authority ports.RevocationAuthority, now func() time.Time) (*RevocationEvaluator, error) {
	if next == nil || authority == nil || now == nil {
		return nil, errors.New("revocation evaluator dependencies are unavailable")
	}
	return &RevocationEvaluator{next, authority, now}, nil
}
func (e *RevocationEvaluator) Decide(ctx context.Context, in Input) (Result, error) {
	tenant, err := domain.ParseID(in.Subject.TenantID)
	if err != nil {
		return Result{}, domain.NewError(domain.CodeUnavailable, "revocation authority input is invalid").WithRetryable()
	}
	var agentID domain.ID
	if in.Agent != nil {
		agentID, _ = domain.ParseID(in.Agent.ID)
	}
	state, err := e.authority.AuthoritativeRevocations(ctx, tenant, agentID, e.now().UTC())
	if err != nil {
		return Result{}, domain.WrapError(domain.CodeUnavailable, "revocation authority is unavailable", err).WithRetryable()
	}
	agentEpoch := state.Epochs.AgentRevocation
	if !in.SecurityState.Authoritative && (in.SecurityState.GlobalEpoch != state.Epochs.Security || in.SecurityState.TenantPolicyEpoch != state.Epochs.TenantPolicy || in.SecurityState.TenantRevocationEpoch != state.Epochs.TenantRevocation || (in.Agent != nil && in.SecurityState.AgentRevocationEpoch != agentEpoch)) {
		return denied(in, "security_state.stale"), nil
	}
	in.SecurityState.GlobalEpoch = state.Epochs.Security
	in.SecurityState.TenantPolicyEpoch = state.Epochs.TenantPolicy
	in.SecurityState.TenantRevocationEpoch = state.Epochs.TenantRevocation
	in.SecurityState.AgentRevocationEpoch = agentEpoch
	in.SecurityState.AgeSeconds = 0
	in.SecurityState.HasGap = false
	in.SecurityState.Authoritative = true
	for _, v := range state.Active {
		if revocationMatches(v, in) {
			if in.Agent != nil && (v.Scope == domain.RevocationAgentID || v.Scope == domain.RevocationAgentVersion) {
				in.Agent.Revoked = true
			}
			reason := "agent.revoked"
			if v.Scope == domain.RevocationPrincipalID {
				reason = "principal.revoked"
			}
			if v.Scope == domain.RevocationPolicyVersion || v.Scope == domain.RevocationTenantID || v.Scope == domain.RevocationGlobal {
				reason = "policy.revoked"
			}
			return denied(in, reason), nil
		}
	}
	return e.next.Decide(ctx, in)
}
func denied(in Input, reason string) Result {
	return Result{Decision: Decision{ContractVersion: ContractVersion, DecisionID: in.DecisionID, Allow: false, ReasonCodes: []string{reason}, ResolvedConstraints: map[string]any{}, Obligations: []Obligation{}, DecisionTTLSeconds: 0}}
}
func revocationMatches(v domain.Revocation, in Input) bool {
	switch v.Scope {
	case domain.RevocationGlobal:
		return true
	case domain.RevocationTenantID:
		return v.Target == in.Subject.TenantID
	case domain.RevocationPrincipalID:
		return v.Target == in.Subject.PrincipalID
	case domain.RevocationRunID:
		return v.Target == in.Resource.ID && in.Resource.Type == "run"
	case domain.RevocationAgentID:
		return in.Agent != nil && v.Target == in.Agent.ID
	case domain.RevocationAgentVersion:
		return in.Agent != nil && v.Target == in.Agent.VersionDigest
	case domain.RevocationPolicyVersion:
		return attribute(in, "policy_version", v.Target)
	case domain.RevocationSkillDigest:
		return attribute(in, "skill_digest", v.Target)
	case domain.RevocationToolID:
		return attribute(in, "tool_id", v.Target)
	}
	return false
}
func attribute(in Input, key, want string) bool {
	v, ok := in.Resource.Attributes[key]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case string:
		return x == want
	case []string:
		for _, s := range x {
			if s == want {
				return true
			}
		}
	case []any:
		for _, s := range x {
			if strings.TrimSpace(want) == strings.TrimSpace(asString(s)) {
				return true
			}
		}
	}
	return false
}
func asString(v any) string { s, _ := v.(string); return s }
