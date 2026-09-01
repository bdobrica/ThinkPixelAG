CREATE TABLE break_glass_grants (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    principal_id uuid NOT NULL,
    approval_id uuid NOT NULL,
    scope text NOT NULL CHECK (scope IN ('POLICY_RECOVERY','REVOCATION_RECOVERY')),
    resource_type text NOT NULL CHECK (length(resource_type) BETWEEN 1 AND 128),
    resource_id text NOT NULL CHECK (length(resource_id) BETWEEN 1 AND 512),
    reason_code text NOT NULL CHECK (length(reason_code) BETWEEN 1 AND 128),
    grant_digest text NOT NULL CHECK (grant_digest ~ '^sha256:[0-9a-f]{64}$'),
    credential_digest text NOT NULL CHECK (credential_digest ~ '^sha256:[0-9a-f]{64}$'),
    strong_authentication_reference text NOT NULL CHECK (length(strong_authentication_reference) BETWEEN 1 AND 512),
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > issued_at AND expires_at <= issued_at + interval '15 minutes'),
    UNIQUE (tenant_id,id), UNIQUE (tenant_id,credential_digest), UNIQUE (tenant_id,approval_id),
    FOREIGN KEY (tenant_id,principal_id) REFERENCES principals(tenant_id,id),
    FOREIGN KEY (tenant_id,approval_id,grant_digest) REFERENCES governance_approval_requests(tenant_id,id,request_digest)
);

CREATE TABLE break_glass_events (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    grant_id uuid NOT NULL,
    actor_principal_id uuid,
    change text NOT NULL CHECK (change IN ('ACTIVATED','USED','EXPIRED','REVOKED')),
    occurred_at timestamptz NOT NULL,
    event jsonb NOT NULL CHECK (jsonb_typeof(event) = 'object'),
    FOREIGN KEY (tenant_id,grant_id) REFERENCES break_glass_grants(tenant_id,id),
    FOREIGN KEY (tenant_id,actor_principal_id) REFERENCES principals(tenant_id,id)
);

CREATE UNIQUE INDEX break_glass_one_terminal_event_idx ON break_glass_events(grant_id)
    WHERE change IN ('EXPIRED','REVOKED');
CREATE INDEX break_glass_active_expiry_idx ON break_glass_grants(tenant_id,expires_at,id);

CREATE FUNCTION reject_break_glass_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'break-glass records are append-only'; END $$;
CREATE TRIGGER break_glass_grants_immutable BEFORE UPDATE OR DELETE ON break_glass_grants FOR EACH ROW EXECUTE FUNCTION reject_break_glass_mutation();
CREATE TRIGGER break_glass_events_immutable BEFORE UPDATE OR DELETE ON break_glass_events FOR EACH ROW EXECUTE FUNCTION reject_break_glass_mutation();

---- create above / drop below ----
DROP TRIGGER break_glass_events_immutable ON break_glass_events;
DROP TRIGGER break_glass_grants_immutable ON break_glass_grants;
DROP FUNCTION reject_break_glass_mutation();
DROP TABLE break_glass_events;
DROP TABLE break_glass_grants;
