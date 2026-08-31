package domain

import (
	"errors"
	"strings"
	"time"
)

// GovernanceApprovalAction is the closed set of operations that require an
// independent human approval before execution.
type GovernanceApprovalAction string

const (
	ApprovalTrustRootRotation      GovernanceApprovalAction = "TRUST_ROOT_ROTATION"
	ApprovalGlobalRevocationChange GovernanceApprovalAction = "GLOBAL_REVOCATION_CHANGE"
	ApprovalPolicyBypass           GovernanceApprovalAction = "POLICY_BYPASS"
	ApprovalPolicyRollback         GovernanceApprovalAction = "POLICY_ROLLBACK"
	ApprovalEmergencyExpansion     GovernanceApprovalAction = "EMERGENCY_EXPANSION"
	ApprovalPrivilegedAgentClass   GovernanceApprovalAction = "PRIVILEGED_AGENT_CLASS"
)

type GovernanceApprovalState string

const (
	GovernanceApprovalPending  GovernanceApprovalState = "PENDING"
	GovernanceApprovalApproved GovernanceApprovalState = "APPROVED"
	GovernanceApprovalRejected GovernanceApprovalState = "REJECTED"
	GovernanceApprovalConsumed GovernanceApprovalState = "CONSUMED"
	GovernanceApprovalExpired  GovernanceApprovalState = "EXPIRED"
)

type GovernanceApproval struct {
	ID, TenantID, RequesterPrincipalID ID
	Action                             GovernanceApprovalAction
	ResourceType, ResourceID           string
	RequestDigest, ReasonCode          string
	Provider, ProviderReference        string
	State                              GovernanceApprovalState
	ApproverPrincipalID                ID
	DecisionReference                  string
	RequestedAt, ExpiresAt             time.Time
	DecidedAt, ConsumedAt              *time.Time
}

func (action GovernanceApprovalAction) Valid() bool {
	switch action {
	case ApprovalTrustRootRotation, ApprovalGlobalRevocationChange, ApprovalPolicyBypass,
		ApprovalPolicyRollback, ApprovalEmergencyExpansion, ApprovalPrivilegedAgentClass:
		return true
	default:
		return false
	}
}

func (approval GovernanceApproval) Validate() error {
	if approval.ID.IsZero() || approval.TenantID.IsZero() || approval.RequesterPrincipalID.IsZero() || !approval.Action.Valid() {
		return errors.New("governance approval identifiers and action must be set")
	}
	for _, value := range []string{approval.ResourceType, approval.ResourceID, approval.RequestDigest, approval.ReasonCode, approval.Provider, approval.ProviderReference} {
		if value == "" || strings.TrimSpace(value) != value {
			return errors.New("governance approval references must be non-empty and canonical")
		}
	}
	if len(approval.ResourceType) > 128 || len(approval.ResourceID) > 512 || len(approval.RequestDigest) != 71 ||
		!strings.HasPrefix(approval.RequestDigest, "sha256:") || len(approval.ReasonCode) > 128 || len(approval.Provider) > 128 || len(approval.ProviderReference) > 512 {
		return errors.New("governance approval references exceed their contract bounds")
	}
	if approval.RequestedAt.IsZero() || approval.ExpiresAt.IsZero() || !approval.ExpiresAt.After(approval.RequestedAt) {
		return errors.New("governance approval validity window is invalid")
	}
	if _, err := RequireUTC(approval.RequestedAt); err != nil {
		return errors.New("governance approval request time must be UTC")
	}
	if _, err := RequireUTC(approval.ExpiresAt); err != nil {
		return errors.New("governance approval expiry time must be UTC")
	}
	return approval.validateState()
}

func (approval GovernanceApproval) validateState() error {
	switch approval.State {
	case GovernanceApprovalPending:
		if !approval.ApproverPrincipalID.IsZero() || approval.DecidedAt != nil || approval.ConsumedAt != nil || approval.DecisionReference != "" {
			return errors.New("pending governance approval contains decision state")
		}
	case GovernanceApprovalApproved, GovernanceApprovalRejected:
		if approval.ApproverPrincipalID.IsZero() || approval.ApproverPrincipalID == approval.RequesterPrincipalID || approval.DecidedAt == nil || approval.DecisionReference == "" || approval.ConsumedAt != nil {
			return errors.New("governance approval decision does not satisfy four-eyes state")
		}
	case GovernanceApprovalConsumed:
		if approval.ApproverPrincipalID.IsZero() || approval.ApproverPrincipalID == approval.RequesterPrincipalID || approval.DecidedAt == nil || approval.ConsumedAt == nil || approval.DecisionReference == "" || approval.ConsumedAt.Before(*approval.DecidedAt) {
			return errors.New("consumed governance approval is invalid")
		}
	case GovernanceApprovalExpired:
		if !approval.ApproverPrincipalID.IsZero() || approval.DecidedAt != nil || approval.ConsumedAt != nil || approval.DecisionReference != "" {
			return errors.New("expired governance approval contains decision state")
		}
	default:
		return errors.New("governance approval state is invalid")
	}
	return nil
}

func (approval GovernanceApproval) Decide(approver ID, approved bool, reference string, at time.Time) (GovernanceApproval, error) {
	if err := approval.Validate(); err != nil || approval.State != GovernanceApprovalPending {
		return GovernanceApproval{}, errors.New("only a valid pending governance approval can be decided")
	}
	if approver.IsZero() || approver == approval.RequesterPrincipalID || strings.TrimSpace(reference) == "" || len(reference) > 512 {
		return GovernanceApproval{}, errors.New("governance approval requires an independent approver and decision reference")
	}
	at, err := RequireUTC(at)
	if err != nil || at.Before(approval.RequestedAt) || !at.Before(approval.ExpiresAt) {
		return GovernanceApproval{}, errors.New("governance approval decision is outside its validity window")
	}
	approval.State = GovernanceApprovalRejected
	if approved {
		approval.State = GovernanceApprovalApproved
	}
	approval.ApproverPrincipalID, approval.DecisionReference, approval.DecidedAt = approver, reference, &at
	return approval, approval.Validate()
}

func (approval GovernanceApproval) Consume(digest string, at time.Time) (GovernanceApproval, error) {
	if err := approval.Validate(); err != nil || approval.State != GovernanceApprovalApproved || digest != approval.RequestDigest {
		return GovernanceApproval{}, errors.New("only a matching approved governance approval can be consumed")
	}
	at, err := RequireUTC(at)
	if err != nil || approval.DecidedAt == nil || at.Before(*approval.DecidedAt) || !at.Before(approval.ExpiresAt) {
		return GovernanceApproval{}, errors.New("governance approval consumption is outside its validity window")
	}
	approval.State, approval.ConsumedAt = GovernanceApprovalConsumed, &at
	return approval, approval.Validate()
}

// EffectiveState derives expiry without mutating or erasing the append-only
// request history.
func (approval GovernanceApproval) EffectiveState(at time.Time) GovernanceApprovalState {
	if approval.State == GovernanceApprovalPending && !at.Before(approval.ExpiresAt) {
		return GovernanceApprovalExpired
	}
	return approval.State
}
