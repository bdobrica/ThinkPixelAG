package thinkpixelag.authorization

import rego.v1

decision := allow_result if allow_result
else := deny("tenant.mismatch") if not tenant_ok
else := deny("security_state.gap") if input.security_state.has_gap
else := deny("security_state.stale") if not operation_fresh
else := deny("action.not_permitted")

tenant_ok if input.subject.tenant_id == input.resource.tenant_id
operation_fresh if { not input.security_state.has_gap; input.security_state.authoritative }
operation_fresh if { not input.security_state.has_gap; input.action in {"agents.list", "runs.create", "runs.signal", "runs.cancel", "runs.claim", "runs.heartbeat", "runs.complete"}; input.security_state.age_seconds <= 30 }
operation_fresh if { not input.security_state.has_gap; input.action in {"agents.describe", "runs.read", "runs.events.read"}; input.security_state.age_seconds <= 60 }
has_role(role) if role in input.subject.roles
is_workload if input.subject.principal_type == "workload"
is_gateway if input.subject.principal_type == "gateway"
agent_ready if { input.agent.approved; not input.agent.revoked }

allow_result := allow("agent.discover.allowed", 10) if { input.contract_version == "thinkpixelag.authorization/v1alpha1"; tenant_ok; operation_fresh; input.action in {"agents.list", "agents.describe"}; has_role("agent-invoker") }
allow_result := allow("agent.invoke.allowed", ttl) if { input.contract_version == "thinkpixelag.authorization/v1alpha1"; tenant_ok; operation_fresh; input.action == "runs.create"; has_role("agent-invoker"); agent_ready; input.agent.risk_class in {"low", "medium"}; ttl := 10 }
allow_result := allow("agent.invoke.allowed", 0) if { input.contract_version == "thinkpixelag.authorization/v1alpha1"; tenant_ok; not input.security_state.has_gap; input.security_state.authoritative; input.action == "runs.create"; has_role("agent-invoker"); agent_ready; input.agent.risk_class in {"high", "critical"} }
allow_result := allow("run.access.allowed", 5) if { tenant_ok; operation_fresh; input.action in {"runs.read", "runs.events.read", "runs.signal", "runs.cancel"}; has_role("agent-invoker") }
allow_result := allow("workload.operation.allowed", 5) if { is_workload; tenant_ok; operation_fresh; input.action in {"runs.claim", "runs.heartbeat", "runs.complete"}; input.agent.risk_class in {"low", "medium"}; has_role("trusted-workload") }
allow_result := allow("workload.operation.allowed", 0) if { is_workload; tenant_ok; input.security_state.authoritative; input.action in {"runs.claim", "runs.heartbeat", "runs.complete"}; input.agent.risk_class in {"high", "critical"}; has_role("trusted-workload") }
allow_result := allow("workload.operation.allowed", 0) if { is_workload; tenant_ok; input.security_state.authoritative; input.action == "resources.settle"; has_role("trusted-settler") }
allow_result := allow_with_obligations("workload.operation.allowed", 0, [budget_exhaustion_obligation]) if { is_workload; tenant_ok; input.security_state.authoritative; input.action == "resources.meter"; has_role("trusted-meter") }
allow_result := allow("governance.operation.allowed", 0) if { tenant_ok; input.security_state.authoritative; input.action in {"agents.manage", "versions.approve", "versions.pin", "versions.rollback"}; has_role("registry-admin") }
allow_result := allow("governance.operation.allowed", 0) if { tenant_ok; input.security_state.authoritative; input.action in {"policies.manage", "policies.activate"}; has_role("policy-admin") }
allow_result := allow("governance.operation.allowed", 0) if { tenant_ok; input.security_state.authoritative; input.action in {"revocations.manage", "revocations.create", "revocations.lift"}; has_role("revocation-admin") }
allow_result := allow("resource.extension.approved", 0) if { tenant_ok; input.security_state.authoritative; input.action == "resources.extend"; has_role("resource-admin"); input.subject.principal_id != input.resource.attributes.requested_by }
allow_result := allow("workload.operation.allowed", 0) if { is_gateway; tenant_ok; input.security_state.authoritative; input.action == "revocations.reconcile"; has_role("trusted-gateway") }

allow(reason, ttl) := {"contract_version": "thinkpixelag.authorization/v1alpha1", "decision_id": input.decision_id, "allow": true, "reason_codes": [reason], "resolved_constraints": resolved, "obligations": [], "decision_ttl_seconds": ttl} if resolved := narrow_constraints
allow_with_obligations(reason, ttl, obligations) := {"contract_version": "thinkpixelag.authorization/v1alpha1", "decision_id": input.decision_id, "allow": true, "reason_codes": [reason], "resolved_constraints": resolved, "obligations": obligations, "decision_ttl_seconds": ttl} if resolved := narrow_constraints
deny(reason) := {"contract_version": "thinkpixelag.authorization/v1alpha1", "decision_id": input.decision_id, "allow": false, "reason_codes": [reason], "resolved_constraints": {}, "obligations": [], "decision_ttl_seconds": 0}

budget_exhaustion_obligation := {"type": "budget.pause_on_exhaustion", "mandatory": false} if input.agent.risk_class in {"low", "medium"}
else := {"type": "budget.fail_on_exhaustion", "mandatory": false}

default narrow_constraints := {}
narrow_constraints := {"max_tokens": value} if {
    requested := input.requested_constraints.max_tokens
    ceiling := input.authority_constraints.max_tokens
    value := min({requested, ceiling})
}
