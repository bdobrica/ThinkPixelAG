package domain

import (
	"errors"
	"regexp"
	"time"
)

var ErrInvalidRevocation = errors.New("invalid revocation")

type RevocationScope string

const (
	RevocationRunID         RevocationScope = "RUN_ID"
	RevocationAgentID       RevocationScope = "AGENT_ID"
	RevocationAgentVersion  RevocationScope = "AGENT_VERSION"
	RevocationSkillDigest   RevocationScope = "SKILL_DIGEST"
	RevocationPrincipalID   RevocationScope = "PRINCIPAL_ID"
	RevocationTenantID      RevocationScope = "TENANT_ID"
	RevocationToolID        RevocationScope = "TOOL_ID"
	RevocationPolicyVersion RevocationScope = "POLICY_VERSION"
	RevocationGlobal        RevocationScope = "GLOBAL"
)

func (s RevocationScope) Valid() bool {
	switch s {
	case RevocationRunID, RevocationAgentID, RevocationAgentVersion, RevocationSkillDigest, RevocationPrincipalID, RevocationTenantID, RevocationToolID, RevocationPolicyVersion, RevocationGlobal:
		return true
	default:
		return false
	}
}

var revocationReasonPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
var revocationReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,511}$`)

type RevocationChangeType string

const (
	RevocationCreated RevocationChangeType = "CREATED"
	RevocationLifted  RevocationChangeType = "LIFTED"
)

type Revocation struct {
	ID, ActorPrincipalID                                   ID
	TenantID                                               *ID
	Scope                                                  RevocationScope
	Target, ReasonCode, DetailReference, ApprovalReference string
	EffectiveAt                                            time.Time
	ExpiresAt                                              *time.Time
	CreatedAt                                              time.Time
}

func ValidateRevocation(v Revocation) (Revocation, error) {
	if v.ID.IsZero() || v.ActorPrincipalID.IsZero() || !v.Scope.Valid() || len(v.Target) < 1 || len(v.Target) > 512 || !revocationReasonPattern.MatchString(v.ReasonCode) || len(v.DetailReference) > 1024 || len(v.ApprovalReference) > 512 || (v.ApprovalReference != "" && !revocationReferencePattern.MatchString(v.ApprovalReference)) || (v.Scope == RevocationGlobal) != (v.TenantID == nil) {
		return Revocation{}, ErrInvalidRevocation
	}
	var err error
	if v.EffectiveAt, err = RequireUTC(v.EffectiveAt); err != nil || v.EffectiveAt.IsZero() {
		return Revocation{}, ErrInvalidRevocation
	}
	if v.CreatedAt, err = RequireUTC(v.CreatedAt); err != nil || v.CreatedAt.IsZero() {
		return Revocation{}, ErrInvalidRevocation
	}
	if v.ExpiresAt != nil {
		expiry, e := RequireUTC(*v.ExpiresAt)
		if e != nil || !expiry.After(v.EffectiveAt) {
			return Revocation{}, ErrInvalidRevocation
		}
		v.ExpiresAt = &expiry
	}
	return v, nil
}

type RevocationLift struct {
	RevocationID, ActorPrincipalID ID
	TenantID                       *ID
	ReasonCode, ApprovalReference  string
	ChangedAt                      time.Time
}

func ValidateRevocationLift(v RevocationLift) (RevocationLift, error) {
	if v.RevocationID.IsZero() || v.ActorPrincipalID.IsZero() || !revocationReasonPattern.MatchString(v.ReasonCode) || !revocationReferencePattern.MatchString(v.ApprovalReference) {
		return RevocationLift{}, ErrInvalidRevocation
	}
	t, err := RequireUTC(v.ChangedAt)
	if err != nil || t.IsZero() {
		return RevocationLift{}, ErrInvalidRevocation
	}
	v.ChangedAt = t
	return v, nil
}

type EpochVector struct {
	Security         int64 `json:"security_epoch"`
	TenantPolicy     int64 `json:"tenant_policy_epoch"`
	TenantRevocation int64 `json:"tenant_revocation_epoch"`
	AgentRevocation  int64 `json:"agent_revocation_epoch"`
}
type RevocationResult struct {
	RevocationID ID
	Revocation   Revocation
	State        RevocationChangeType
	Sequence     int64
	Epochs       EpochVector
}
