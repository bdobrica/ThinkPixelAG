package domain

import "testing"

func TestAgentVersionApprovalTransitions(t *testing.T) {
	tests := []struct {
		from AgentVersionState
		to   AgentVersionDecision
		want bool
	}{
		{AgentVersionRegistered, DecisionApprove, true}, {AgentVersionRegistered, DecisionReject, true},
		{AgentVersionApproved, DecisionDeprecate, true}, {AgentVersionApproved, DecisionRevoke, true},
		{AgentVersionDeprecated, DecisionRevoke, true}, {AgentVersionRejected, DecisionApprove, false},
		{AgentVersionRevoked, DecisionApprove, false}, {AgentVersionRegistered, DecisionRevoke, false},
	}
	for _, test := range tests {
		if got := test.from.CanApply(test.to); got != test.want {
			t.Errorf("%s -> %s = %v, want %v", test.from, test.to, got, test.want)
		}
	}
}
