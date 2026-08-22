package domain

import (
	"errors"
	"strings"
	"time"
)

type AgentVersionState string

const (
	AgentVersionRegistered AgentVersionState = "REGISTERED"
	AgentVersionApproved   AgentVersionState = "APPROVED"
	AgentVersionRejected   AgentVersionState = "REJECTED"
	AgentVersionDeprecated AgentVersionState = "DEPRECATED"
	AgentVersionRevoked    AgentVersionState = "REVOKED"
)

type AgentVersionDecision string

const (
	DecisionApprove   AgentVersionDecision = "APPROVED"
	DecisionReject    AgentVersionDecision = "REJECTED"
	DecisionDeprecate AgentVersionDecision = "DEPRECATED"
	DecisionRevoke    AgentVersionDecision = "REVOKED"
)

type AgentVersionApproval struct {
	ID, TenantID, AgentID, AgentVersionID, ActorPrincipalID ID
	PolicyDecisionID                                        ID
	Decision                                                AgentVersionDecision
	ReasonCode, ApprovalReference                           string
	CreatedAt                                               time.Time
}

func (approval AgentVersionApproval) Validate() error {
	if approval.ID.IsZero() || approval.TenantID.IsZero() || approval.AgentID.IsZero() || approval.AgentVersionID.IsZero() || approval.ActorPrincipalID.IsZero() || approval.PolicyDecisionID.IsZero() {
		return errors.New("agent version approval identifiers must be set")
	}
	if !approval.Decision.Valid() || !validDeclaration(approval.ReasonCode, 128, true) {
		return errors.New("agent version approval decision or reason code is invalid")
	}
	if len(approval.ApprovalReference) > 512 || strings.TrimSpace(approval.ApprovalReference) != approval.ApprovalReference {
		return errors.New("agent version approval reference is invalid")
	}
	if _, err := RequireUTC(approval.CreatedAt); err != nil || approval.CreatedAt.IsZero() {
		return errors.New("agent version approval time must be a non-zero UTC timestamp")
	}
	return nil
}

func (decision AgentVersionDecision) Valid() bool {
	switch decision {
	case DecisionApprove, DecisionReject, DecisionDeprecate, DecisionRevoke:
		return true
	default:
		return false
	}
}

func (state AgentVersionState) CanApply(decision AgentVersionDecision) bool {
	switch state {
	case AgentVersionRegistered:
		return decision == DecisionApprove || decision == DecisionReject
	case AgentVersionApproved:
		return decision == DecisionDeprecate || decision == DecisionRevoke
	case AgentVersionDeprecated:
		return decision == DecisionRevoke
	default:
		return false
	}
}

func (decision AgentVersionDecision) State() AgentVersionState { return AgentVersionState(decision) }
