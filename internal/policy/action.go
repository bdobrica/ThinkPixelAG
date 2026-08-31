package policy

import "strings"

// ActionRisk is the closed security classification for an authorization
// action. Unknown actions are privileged so new operations fail conservatively.
type ActionRisk string

const (
	ActionRiskLowRead         ActionRisk = "low_read"
	ActionRiskSensitiveRead   ActionRisk = "sensitive_read"
	ActionRiskNormalWrite     ActionRisk = "normal_write"
	ActionRiskPrivilegedWrite ActionRisk = "privileged_write"
)

// ClassifyAction is the canonical action-risk catalog used by application
// freshness enforcement and documented by the authorization contract.
func ClassifyAction(action, agentRisk string) ActionRisk {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "agents.list":
		return ActionRiskLowRead
	case "agents.describe", "runs.read", "runs.events.read":
		return ActionRiskSensitiveRead
	case "runs.signal", "runs.cancel":
		return ActionRiskNormalWrite
	case "runs.create", "runs.claim", "runs.heartbeat", "runs.complete":
		if risk := strings.ToLower(strings.TrimSpace(agentRisk)); risk == "low" || risk == "medium" {
			return ActionRiskNormalWrite
		}
		return ActionRiskPrivilegedWrite
	case "resources.meter", "resources.settle", "resources.extend",
		"agents.manage", "versions.approve", "versions.pin", "versions.rollback",
		"policies.manage", "policies.activate",
		"revocations.manage", "revocations.create", "revocations.lift", "revocations.reconcile":
		return ActionRiskPrivilegedWrite
	default:
		return ActionRiskPrivilegedWrite
	}
}
