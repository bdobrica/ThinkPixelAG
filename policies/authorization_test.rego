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
