CREATE TABLE security_epochs (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    security_epoch bigint NOT NULL DEFAULT 0 CHECK (security_epoch >= 0)
);

INSERT INTO security_epochs (singleton, security_epoch) VALUES (true, 0);

CREATE TABLE tenant_security_epochs (
    tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
    policy_epoch bigint NOT NULL DEFAULT 0 CHECK (policy_epoch >= 0),
    revocation_epoch bigint NOT NULL DEFAULT 0 CHECK (revocation_epoch >= 0)
);

CREATE TABLE agent_security_epochs (
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    revocation_epoch bigint NOT NULL DEFAULT 0 CHECK (revocation_epoch >= 0),
    PRIMARY KEY (tenant_id, agent_id),
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents (tenant_id, id)
);

CREATE TABLE revocations (
    id uuid PRIMARY KEY,
    tenant_id uuid REFERENCES tenants(id),
    scope text NOT NULL CHECK (scope IN (
        'RUN_ID', 'AGENT_ID', 'AGENT_VERSION', 'SKILL_DIGEST', 'PRINCIPAL_ID',
        'TENANT_ID', 'TOOL_ID', 'POLICY_VERSION', 'GLOBAL'
    )),
    target text NOT NULL CHECK (length(target) BETWEEN 1 AND 512),
    reason_code text NOT NULL CHECK (length(reason_code) BETWEEN 1 AND 128),
    detail_reference text CHECK (detail_reference IS NULL OR length(detail_reference) <= 1024),
    actor_principal_id uuid,
    approval_reference text CHECK (approval_reference IS NULL OR length(approval_reference) <= 512),
    effective_at timestamptz NOT NULL,
    expires_at timestamptz,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, actor_principal_id) REFERENCES principals (tenant_id, id),
    CHECK ((scope = 'GLOBAL') = (tenant_id IS NULL)),
    CHECK (expires_at IS NULL OR expires_at > effective_at)
);

CREATE INDEX revocations_tenant_scope_target_idx
    ON revocations (tenant_id, scope, target, effective_at DESC, id);
CREATE INDEX revocations_global_target_idx
    ON revocations (scope, target, effective_at DESC, id) WHERE tenant_id IS NULL;
CREATE INDEX revocations_expiry_idx
    ON revocations (expires_at, id) WHERE expires_at IS NOT NULL;

CREATE TABLE revocation_changes (
    id uuid PRIMARY KEY,
    revocation_id uuid NOT NULL REFERENCES revocations(id),
    tenant_id uuid,
    change_type text NOT NULL CHECK (change_type IN ('CREATED', 'LIFTED', 'EXPIRED')),
    actor_principal_id uuid,
    reason_code text NOT NULL CHECK (length(reason_code) BETWEEN 1 AND 128),
    changed_at timestamptz NOT NULL,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, actor_principal_id) REFERENCES principals (tenant_id, id)
);

CREATE INDEX revocation_changes_revocation_time_idx
    ON revocation_changes (revocation_id, changed_at, id);

CREATE TABLE revocation_log (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id uuid NOT NULL UNIQUE,
    revocation_id uuid NOT NULL REFERENCES revocations(id),
    change_id uuid NOT NULL UNIQUE REFERENCES revocation_changes(id),
    tenant_id uuid,
    scope text NOT NULL CHECK (scope IN (
        'RUN_ID', 'AGENT_ID', 'AGENT_VERSION', 'SKILL_DIGEST', 'PRINCIPAL_ID',
        'TENANT_ID', 'TOOL_ID', 'POLICY_VERSION', 'GLOBAL'
    )),
    target text NOT NULL CHECK (length(target) BETWEEN 1 AND 512),
    change_type text NOT NULL CHECK (change_type IN ('CREATED', 'LIFTED', 'EXPIRED')),
    security_epoch bigint NOT NULL CHECK (security_epoch >= 0),
    tenant_policy_epoch bigint CHECK (tenant_policy_epoch IS NULL OR tenant_policy_epoch >= 0),
    tenant_revocation_epoch bigint CHECK (tenant_revocation_epoch IS NULL OR tenant_revocation_epoch >= 0),
    agent_revocation_epoch bigint CHECK (agent_revocation_epoch IS NULL OR agent_revocation_epoch >= 0),
    committed_at timestamptz NOT NULL,
    CHECK ((scope = 'GLOBAL') = (tenant_id IS NULL))
);

CREATE INDEX revocation_log_tenant_sequence_idx ON revocation_log (tenant_id, sequence);

CREATE TABLE gateway_checkpoints (
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    gateway_principal_id uuid NOT NULL,
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    security_epoch bigint NOT NULL DEFAULT 0 CHECK (security_epoch >= 0),
    tenant_policy_epoch bigint NOT NULL DEFAULT 0 CHECK (tenant_policy_epoch >= 0),
    tenant_revocation_epoch bigint NOT NULL DEFAULT 0 CHECK (tenant_revocation_epoch >= 0),
    last_stream_received_at timestamptz,
    last_reconciled_at timestamptz,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, gateway_principal_id),
    FOREIGN KEY (tenant_id, gateway_principal_id) REFERENCES principals (tenant_id, id)
);

CREATE INDEX gateway_checkpoints_tenant_sequence_idx
    ON gateway_checkpoints (tenant_id, last_sequence, gateway_principal_id);

---- create above / drop below ----

DROP TABLE gateway_checkpoints;
DROP TABLE revocation_log;
DROP TABLE revocation_changes;
DROP TABLE revocations;
DROP TABLE agent_security_epochs;
DROP TABLE tenant_security_epochs;
DROP TABLE security_epochs;
