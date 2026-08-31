package policy

import "testing"

func TestClassifyActionIsClosedAndRiskSensitive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action, agentRisk string
		want              ActionRisk
	}{
		{"agents.list", "", ActionRiskLowRead},
		{"runs.read", "", ActionRiskSensitiveRead},
		{"runs.cancel", "", ActionRiskNormalWrite},
		{"runs.create", "medium", ActionRiskNormalWrite},
		{"runs.create", "high", ActionRiskPrivilegedWrite},
		{"runs.claim", "critical", ActionRiskPrivilegedWrite},
		{"resources.meter", "low", ActionRiskPrivilegedWrite},
		{"policies.activate", "", ActionRiskPrivilegedWrite},
		{"future.action", "", ActionRiskPrivilegedWrite},
	}
	for _, test := range tests {
		if got := ClassifyAction(test.action, test.agentRisk); got != test.want {
			t.Errorf("ClassifyAction(%q, %q) = %q, want %q", test.action, test.agentRisk, got, test.want)
		}
	}
}
