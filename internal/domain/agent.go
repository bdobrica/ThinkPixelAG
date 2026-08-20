package domain

import (
	"errors"
	"strings"
	"time"
)

type AgentRiskClass string

const (
	AgentRiskLow      AgentRiskClass = "LOW"
	AgentRiskMedium   AgentRiskClass = "MEDIUM"
	AgentRiskHigh     AgentRiskClass = "HIGH"
	AgentRiskCritical AgentRiskClass = "CRITICAL"
)

type AgentStatus string

const (
	AgentActive    AgentStatus = "ACTIVE"
	AgentSuspended AgentStatus = "SUSPENDED"
	AgentRetired   AgentStatus = "RETIRED"
)

// Agent is the mutable registry record. Implementations are immutable and are
// represented separately by agent versions.
type Agent struct {
	ID                 ID
	TenantID           ID
	Name               string
	Description        string
	OwnerPrincipalID   ID
	SponsorPrincipalID ID
	RiskClass          AgentRiskClass
	Status             AgentStatus
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (agent Agent) Validate() error {
	if agent.ID.IsZero() || agent.TenantID.IsZero() || agent.OwnerPrincipalID.IsZero() || agent.SponsorPrincipalID.IsZero() {
		return errors.New("agent identifiers must be set")
	}
	if agent.OwnerPrincipalID == agent.SponsorPrincipalID {
		return errors.New("agent owner and sponsor must be different principals")
	}
	if agent.Name != strings.TrimSpace(agent.Name) || len(agent.Name) < 1 || len(agent.Name) > 200 {
		return errors.New("agent name must contain 1 to 200 trimmed characters")
	}
	if len(agent.Description) > 2048 {
		return errors.New("agent description exceeds 2048 characters")
	}
	if !agent.RiskClass.Valid() || !agent.Status.Valid() {
		return errors.New("agent risk class or status is invalid")
	}
	created, err := RequireUTC(agent.CreatedAt)
	if err != nil || created.IsZero() {
		return errors.New("agent creation time must be a non-zero UTC timestamp")
	}
	updated, err := RequireUTC(agent.UpdatedAt)
	if err != nil || updated.Before(created) {
		return errors.New("agent update time must be UTC and not precede creation")
	}
	return nil
}

func (risk AgentRiskClass) Valid() bool {
	switch risk {
	case AgentRiskLow, AgentRiskMedium, AgentRiskHigh, AgentRiskCritical:
		return true
	default:
		return false
	}
}

func (status AgentStatus) Valid() bool {
	switch status {
	case AgentActive, AgentSuspended, AgentRetired:
		return true
	default:
		return false
	}
}

func (agent Agent) CanTransitionTo(next AgentStatus) bool {
	if !next.Valid() {
		return false
	}
	if agent.Status == AgentRetired {
		return next == AgentRetired
	}
	return true
}
