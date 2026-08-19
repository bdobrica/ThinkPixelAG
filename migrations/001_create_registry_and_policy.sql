CREATE TABLE tenants (
    id uuid PRIMARY KEY,
    slug text NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    status text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'SUSPENDED', 'RETIRED')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at)
);

CREATE TABLE principals (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    external_issuer text NOT NULL CHECK (length(external_issuer) BETWEEN 1 AND 2048),
    external_subject text NOT NULL CHECK (length(external_subject) BETWEEN 1 AND 512),
    principal_type text NOT NULL CHECK (principal_type IN ('HUMAN', 'WORKLOAD', 'GATEWAY', 'SYSTEM')),
    display_name text CHECK (display_name IS NULL OR length(display_name) BETWEEN 1 AND 200),
    disabled_at timestamptz,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, external_issuer, external_subject),
    UNIQUE (tenant_id, id)
);

CREATE INDEX principals_tenant_type_idx ON principals (tenant_id, principal_type, id);

CREATE TABLE agents (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 4000),
    owner_principal_id uuid NOT NULL,
    sponsor_principal_id uuid,
    risk_class text NOT NULL CHECK (risk_class IN ('LOW', 'MEDIUM', 'HIGH', 'CRITICAL')),
    status text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'SUSPENDED', 'RETIRED')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (tenant_id, name),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, owner_principal_id) REFERENCES principals (tenant_id, id),
    FOREIGN KEY (tenant_id, sponsor_principal_id) REFERENCES principals (tenant_id, id),
    CHECK (updated_at >= created_at)
);

CREATE INDEX agents_tenant_status_idx ON agents (tenant_id, status, id);
CREATE INDEX agents_tenant_owner_idx ON agents (tenant_id, owner_principal_id, id);

CREATE TABLE agent_versions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    content_digest text NOT NULL CHECK (content_digest ~ '^sha256:[0-9a-f]{64}$'),
    image_digest text NOT NULL CHECK (image_digest ~ '^sha256:[0-9a-f]{64}$'),
    manifest_schema_version integer NOT NULL CHECK (manifest_schema_version > 0),
    manifest jsonb NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, agent_id, content_digest),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, agent_id, id),
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents (tenant_id, id),
    FOREIGN KEY (tenant_id, created_by) REFERENCES principals (tenant_id, id)
);

CREATE INDEX agent_versions_tenant_agent_created_idx
    ON agent_versions (tenant_id, agent_id, created_at DESC, id);

CREATE TABLE agent_capabilities (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    capability_type text NOT NULL CHECK (capability_type IN ('MODEL', 'TOOL', 'SKILL', 'SUBAGENT')),
    capability_identifier text NOT NULL CHECK (length(capability_identifier) BETWEEN 1 AND 512),
    constraints jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(constraints) = 'object'),
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, agent_version_id, capability_type, capability_identifier),
    FOREIGN KEY (tenant_id, agent_id, agent_version_id)
        REFERENCES agent_versions (tenant_id, agent_id, id)
);

CREATE INDEX agent_capabilities_tenant_version_idx
    ON agent_capabilities (tenant_id, agent_version_id, capability_type, id);

CREATE TABLE agent_version_approvals (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    decision text NOT NULL CHECK (decision IN ('APPROVED', 'REJECTED', 'DEPRECATED', 'REVOKED')),
    actor_principal_id uuid NOT NULL,
    policy_decision_id uuid,
    reason_code text NOT NULL CHECK (length(reason_code) BETWEEN 1 AND 128),
    approval_reference text CHECK (approval_reference IS NULL OR length(approval_reference) <= 512),
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_id, agent_version_id)
        REFERENCES agent_versions (tenant_id, agent_id, id),
    FOREIGN KEY (tenant_id, actor_principal_id) REFERENCES principals (tenant_id, id)
);

CREATE INDEX agent_version_approvals_tenant_version_time_idx
    ON agent_version_approvals (tenant_id, agent_version_id, created_at DESC, id);

CREATE TABLE policy_bundles (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    channel text NOT NULL CHECK (length(channel) BETWEEN 1 AND 128),
    content_digest text NOT NULL CHECK (content_digest ~ '^sha256:[0-9a-f]{64}$'),
    contract_version text NOT NULL CHECK (length(contract_version) BETWEEN 1 AND 128),
    bundle bytea NOT NULL CHECK (octet_length(bundle) > 0),
    signature bytea NOT NULL CHECK (octet_length(signature) > 0),
    signer_key_id text NOT NULL CHECK (length(signer_key_id) BETWEEN 1 AND 512),
    validation_status text NOT NULL CHECK (validation_status IN ('UPLOADED', 'VALIDATED', 'APPROVED', 'REJECTED')),
    valid_from timestamptz,
    valid_until timestamptz,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, channel, content_digest),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, created_by) REFERENCES principals (tenant_id, id),
    CHECK (valid_until IS NULL OR valid_from IS NULL OR valid_until > valid_from)
);

CREATE INDEX policy_bundles_tenant_channel_status_idx
    ON policy_bundles (tenant_id, channel, validation_status, created_at DESC, id);

CREATE TABLE policy_activations (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    channel text NOT NULL CHECK (length(channel) BETWEEN 1 AND 128),
    policy_bundle_id uuid NOT NULL,
    activation_version bigint NOT NULL CHECK (activation_version > 0),
    actor_principal_id uuid NOT NULL,
    policy_decision_id uuid,
    reason_code text NOT NULL CHECK (length(reason_code) BETWEEN 1 AND 128),
    activated_at timestamptz NOT NULL,
    deactivated_at timestamptz,
    UNIQUE (tenant_id, channel, activation_version),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, policy_bundle_id) REFERENCES policy_bundles (tenant_id, id),
    FOREIGN KEY (tenant_id, actor_principal_id) REFERENCES principals (tenant_id, id),
    CHECK (deactivated_at IS NULL OR deactivated_at >= activated_at)
);

CREATE UNIQUE INDEX policy_activations_one_active_channel_idx
    ON policy_activations (tenant_id, channel) WHERE deactivated_at IS NULL;
CREATE INDEX policy_activations_tenant_channel_version_idx
    ON policy_activations (tenant_id, channel, activation_version DESC);

---- create above / drop below ----

DROP TABLE policy_activations;
DROP TABLE policy_bundles;
DROP TABLE agent_version_approvals;
DROP TABLE agent_capabilities;
DROP TABLE agent_versions;
DROP TABLE agents;
DROP TABLE principals;
DROP TABLE tenants;
