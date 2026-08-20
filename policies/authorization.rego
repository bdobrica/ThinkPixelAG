package thinkpixelag.authorization

import rego.v1

decision := allow_result if allow_result
else := deny("tenant.mismatch") if not tenant_ok
else := deny("security_state.gap") if input.security_state.has_gap
else := deny("security_state.stale") if input.security_state.age_seconds > 30
else := deny("action.not_permitted")

tenant_ok if input.subject.tenant_id == input.resource.tenant_id
fresh if { not input.security_state.has_gap; input.security_state.age_seconds <= 30 }
has_role(role) if role in input.subject.roles
agent_ready if { input.agent.approved; not input.agent.revoked }

allow_result := allow("agent.discover.allowed", 10) if { input.contract_version == "thinkpixelag.authorization/v1alpha1"; tenant_ok; fresh; input.action in {"agents.list", "agents.describe"}; has_role("agent-invoker") }
allow_result := allow("agent.invoke.allowed", ttl) if { input.contract_version == "thinkpixelag.authorization/v1alpha1"; tenant_ok; fresh; input.action == "runs.create"; has_role("agent-invoker"); agent_ready; input.agent.risk_class in {"low", "medium"}; ttl := 10 }
allow_result := allow("agent.invoke.allowed", 0) if { input.contract_version == "thinkpixelag.authorization/v1alpha1"; tenant_ok; not input.security_state.has_gap; input.security_state.authoritative; input.action == "runs.create"; has_role("agent-invoker"); agent_ready; input.agent.risk_class in {"high", "critical"} }
allow_result := allow("run.access.allowed", 5) if { tenant_ok; fresh; input.action in {"runs.read", "runs.events.read", "runs.signal", "runs.cancel"}; has_role("agent-invoker") }
allow_result := allow("workload.operation.allowed", 0) if { tenant_ok; input.security_state.authoritative; input.action in {"runs.claim", "runs.heartbeat", "runs.complete", "resources.meter", "resources.settle"}; has_role("trusted-workload") }
allow_result := allow("governance.operation.allowed", 0) if { tenant_ok; input.security_state.authoritative; input.action in {"agents.manage", "versions.approve", "policies.manage", "policies.activate", "revocations.manage", "resources.extend"}; has_role("governance-admin") }
allow_result := allow("workload.operation.allowed", 0) if { tenant_ok; input.security_state.authoritative; input.action == "revocations.reconcile"; has_role("trusted-gateway") }

allow(reason, ttl) := {"contract_version": "thinkpixelag.authorization/v1alpha1", "decision_id": input.decision_id, "allow": true, "reason_codes": [reason], "resolved_constraints": resolved, "obligations": [], "decision_ttl_seconds": ttl} if resolved := narrow_constraints
deny(reason) := {"contract_version": "thinkpixelag.authorization/v1alpha1", "decision_id": input.decision_id, "allow": false, "reason_codes": [reason], "resolved_constraints": {}, "obligations": [], "decision_ttl_seconds": 0}

default narrow_constraints := {}
narrow_constraints := {"max_tokens": value} if {
    requested := input.requested_constraints.max_tokens
    ceiling := input.authority_constraints.max_tokens
    value := min({requested, ceiling})
}
