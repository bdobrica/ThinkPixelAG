package thinkpixelag.authorization_test
import rego.v1
import data.thinkpixelag.authorization

base := {"contract_version":"thinkpixelag.authorization/v1alpha1","decision_id":"d","subject":{"tenant_id":"t","roles":["agent-invoker"]},"resource":{"tenant_id":"t"},"security_state":{"has_gap":false,"age_seconds":1,"authoritative":true},"requested_constraints":{},"authority_constraints":{},"agent":{"approved":true,"revoked":false,"risk_class":"medium"}}
test_discovery_allowed if {
    d := authorization.decision with input as object.union(base,{"action":"agents.list"})
    d.allow
}
test_cross_tenant_denied if {
    d := authorization.decision with input as object.union(base,{"action":"agents.list","resource":{"tenant_id":"other"}})
    not d.allow
}
test_admin_requires_role if {
    d := authorization.decision with input as object.union(base,{"action":"policies.activate"})
    not d.allow
}
test_stale_denied if {
    d := authorization.decision with input as object.union(base,{"action":"agents.list","security_state":{"has_gap":false,"age_seconds":31,"authoritative":true}})
    not d.allow
}
test_constraints_narrowed if {
    d := authorization.decision with input as object.union(base,{"action":"runs.create","requested_constraints":{"max_tokens":200},"authority_constraints":{"max_tokens":100}})
    d.resolved_constraints.max_tokens == 100
}

test_constraint_request_is_preserved_below_ceiling if {
    d := authorization.decision with input as object.union(base,{"action":"runs.create","requested_constraints":{"max_tokens":40},"authority_constraints":{"max_tokens":100}})
    d.allow
    d.resolved_constraints.max_tokens == 40
}

test_gap_denied if {
    d := authorization.decision with input as object.union(base,{"action":"agents.list","security_state":{"has_gap":true,"age_seconds":1,"authoritative":true}})
    not d.allow
    d.reason_codes == ["security_state.gap"]
}

test_high_risk_requires_authoritative_state if {
    d := authorization.decision with input as object.union(base,{"action":"runs.create","agent":{"approved":true,"revoked":false,"risk_class":"high"},"security_state":{"has_gap":false,"age_seconds":1,"authoritative":false}})
    not d.allow
}

test_high_risk_allow_has_zero_ttl if {
    d := authorization.decision with input as object.union(base,{"action":"runs.create","agent":{"approved":true,"revoked":false,"risk_class":"high"}})
    d.allow
    d.decision_ttl_seconds == 0
}

test_unapproved_agent_denied if {
    d := authorization.decision with input as object.union(base,{"action":"runs.create","agent":{"approved":false,"revoked":false,"risk_class":"medium"}})
    not d.allow
}

test_revoked_agent_denied if {
    d := authorization.decision with input as object.union(base,{"action":"runs.create","agent":{"approved":true,"revoked":true,"risk_class":"medium"}})
    not d.allow
}

test_governance_admin_allowed if {
    d := authorization.decision with input as object.union(base,{"action":"policies.activate","subject":{"tenant_id":"t","roles":["governance-admin"]}})
    d.allow
    d.decision_ttl_seconds == 0
}

test_version_pin_requires_governance_admin if {
    denied := authorization.decision with input as object.union(base,{"action":"versions.pin"})
    not denied.allow
    allowed := authorization.decision with input as object.union(base,{"action":"versions.pin","subject":{"tenant_id":"t","roles":["governance-admin"]}})
    allowed.allow
    allowed.decision_ttl_seconds == 0
}

test_version_rollback_requires_governance_admin if {
    denied := authorization.decision with input as object.union(base,{"action":"versions.rollback"})
    not denied.allow
    allowed := authorization.decision with input as object.union(base,{"action":"versions.rollback","subject":{"tenant_id":"t","roles":["governance-admin"]}})
    allowed.allow
}

test_workload_role_cannot_administer if {
    d := authorization.decision with input as object.union(base,{"action":"policies.activate","subject":{"tenant_id":"t","roles":["trusted-workload"]}})
    not d.allow
}

test_trusted_workload_allowed_to_settle if {
    d := authorization.decision with input as object.union(base,{"action":"resources.settle","subject":{"tenant_id":"t","roles":["trusted-workload"]}})
    d.allow
    d.decision_ttl_seconds == 0
}

test_metering_policy_selects_budget_exhaustion_disposition if {
    low := authorization.decision with input as object.union(base,{"action":"resources.meter","subject":{"tenant_id":"t","roles":["trusted-workload"]},"agent":{"approved":true,"revoked":false,"risk_class":"low"}})
    low.obligations == [{"type":"budget.pause_on_exhaustion","mandatory":false}]
    high := authorization.decision with input as object.union(base,{"action":"resources.meter","subject":{"tenant_id":"t","roles":["trusted-workload"]},"agent":{"approved":true,"revoked":false,"risk_class":"high"}})
    high.obligations == [{"type":"budget.fail_on_exhaustion","mandatory":false}]
}

test_unknown_action_denied if {
    d := authorization.decision with input as object.union(base,{"action":"unknown.action"})
    not d.allow
    d.reason_codes == ["action.not_permitted"]
}

test_malformed_input_has_no_allow if {
    not authorization.decision.allow with input as {"action":"agents.list"}
}

test_deterministic_equivalent_input if {
    first := authorization.decision with input as object.union(base,{"action":"agents.list","subject":{"tenant_id":"t","roles":["other","agent-invoker"]}})
    second := authorization.decision with input as object.union(base,{"action":"agents.list","subject":{"tenant_id":"t","roles":["agent-invoker","other"]}})
    first == second
}
