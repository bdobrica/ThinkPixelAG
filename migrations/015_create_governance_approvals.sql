CREATE TABLE governance_approval_requests (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    requester_principal_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('TRUST_ROOT_ROTATION','GLOBAL_REVOCATION_CHANGE','POLICY_BYPASS','POLICY_ROLLBACK','EMERGENCY_EXPANSION','PRIVILEGED_AGENT_CLASS')),
    resource_type text NOT NULL CHECK (length(resource_type) BETWEEN 1 AND 128),
    resource_id text NOT NULL CHECK (length(resource_id) BETWEEN 1 AND 512),
    request_digest text NOT NULL CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    reason_code text NOT NULL CHECK (length(reason_code) BETWEEN 1 AND 128),
    provider text NOT NULL CHECK (length(provider) BETWEEN 1 AND 128),
    provider_reference text NOT NULL CHECK (length(provider_reference) BETWEEN 1 AND 512),
    requested_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > requested_at),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, id, requester_principal_id),
    UNIQUE (tenant_id, id, request_digest),
    UNIQUE (tenant_id, provider, provider_reference),
    FOREIGN KEY (tenant_id, requester_principal_id) REFERENCES principals (tenant_id, id)
);

CREATE INDEX governance_approval_requests_tenant_action_time_idx
    ON governance_approval_requests (tenant_id, action, requested_at DESC, id);

CREATE TABLE governance_approval_decisions (
    approval_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    requester_principal_id uuid NOT NULL,
    approver_principal_id uuid NOT NULL,
    approved boolean NOT NULL,
    decision_reference text NOT NULL CHECK (length(decision_reference) BETWEEN 1 AND 512),
    decided_at timestamptz NOT NULL,
    CHECK (requester_principal_id <> approver_principal_id),
    UNIQUE (tenant_id, decision_reference),
    FOREIGN KEY (tenant_id, approval_id, requester_principal_id) REFERENCES governance_approval_requests (tenant_id, id, requester_principal_id),
    FOREIGN KEY (tenant_id, approver_principal_id) REFERENCES principals (tenant_id, id)
);

CREATE TABLE governance_approval_consumptions (
    approval_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    request_digest text NOT NULL CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    consumed_at timestamptz NOT NULL,
    FOREIGN KEY (tenant_id, approval_id, request_digest) REFERENCES governance_approval_requests (tenant_id, id, request_digest)
);

CREATE FUNCTION reject_governance_approval_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'governance approval records are append-only'; END $$;

CREATE TRIGGER governance_approval_requests_append_only BEFORE UPDATE OR DELETE ON governance_approval_requests
    FOR EACH ROW EXECUTE FUNCTION reject_governance_approval_mutation();
CREATE TRIGGER governance_approval_decisions_append_only BEFORE UPDATE OR DELETE ON governance_approval_decisions
    FOR EACH ROW EXECUTE FUNCTION reject_governance_approval_mutation();
CREATE TRIGGER governance_approval_consumptions_append_only BEFORE UPDATE OR DELETE ON governance_approval_consumptions
    FOR EACH ROW EXECUTE FUNCTION reject_governance_approval_mutation();

---- create above / drop below ----

DROP TRIGGER governance_approval_consumptions_append_only ON governance_approval_consumptions;
DROP TRIGGER governance_approval_decisions_append_only ON governance_approval_decisions;
DROP TRIGGER governance_approval_requests_append_only ON governance_approval_requests;
DROP FUNCTION reject_governance_approval_mutation();
DROP TABLE governance_approval_consumptions;
DROP TABLE governance_approval_decisions;
DROP TABLE governance_approval_requests;
