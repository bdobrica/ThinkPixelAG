package domain

import (
	"testing"
	"time"
)

func TestAgentValidationAndTransitions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	agent := Agent{ID: mustID(t), TenantID: mustID(t), Name: "payments-review", OwnerPrincipalID: mustID(t), SponsorPrincipalID: mustID(t), RiskClass: AgentRiskHigh, Status: AgentActive, CreatedAt: now, UpdatedAt: now}
	if err := agent.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, mutation := range []func(*Agent){
		func(a *Agent) { a.Name = " padded " },
		func(a *Agent) { a.OwnerPrincipalID = a.SponsorPrincipalID },
		func(a *Agent) { a.RiskClass = "EXTREME" },
		func(a *Agent) { a.Status = "DELETED" },
		func(a *Agent) { a.UpdatedAt = a.CreatedAt.Add(-time.Second) },
	} {
		invalid := agent
		mutation(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatalf("invalid agent accepted: %+v", invalid)
		}
	}
	if !agent.CanTransitionTo(AgentSuspended) || !agent.CanTransitionTo(AgentRetired) {
		t.Fatal("active agent did not permit suspension/retirement")
	}
	agent.Status = AgentRetired
	if agent.CanTransitionTo(AgentActive) || !agent.CanTransitionTo(AgentRetired) {
		t.Fatal("retired terminal-state semantics are invalid")
	}
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
