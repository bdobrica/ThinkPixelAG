CREATE TABLE resource_extensions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    run_id uuid NOT NULL,
    envelope_id uuid NOT NULL,
    actor_principal_id uuid NOT NULL,
    policy_decision_id uuid NOT NULL,
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 256),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z][a-z0-9._-]{0,127}$'),
    approval_reference text NOT NULL CHECK (length(approval_reference) BETWEEN 1 AND 256),
    content_digest text NOT NULL CHECK (content_digest ~ '^sha256:[0-9a-f]{64}$'),
    prior_deadline_at timestamptz,
    new_deadline_at timestamptz,
    prior_envelope_version bigint NOT NULL CHECK (prior_envelope_version > 0),
    new_envelope_version bigint NOT NULL CHECK (new_envelope_version = prior_envelope_version + 1),
    resumed_run boolean NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, actor_principal_id, idempotency_key),
    FOREIGN KEY (tenant_id, run_id) REFERENCES runs (tenant_id, id),
    FOREIGN KEY (tenant_id, envelope_id) REFERENCES resource_envelopes (tenant_id, id),
    FOREIGN KEY (tenant_id, actor_principal_id) REFERENCES principals (tenant_id, id),
    CHECK ((prior_deadline_at IS NULL AND new_deadline_at IS NULL) OR
           (prior_deadline_at IS NOT NULL AND new_deadline_at >= prior_deadline_at))
);

CREATE TABLE resource_extension_items (
    tenant_id uuid NOT NULL,
    extension_id uuid NOT NULL,
    dimension_id uuid NOT NULL,
    added_value bigint NOT NULL CHECK (added_value > 0),
    prior_granted_value bigint NOT NULL CHECK (prior_granted_value >= 0),
    new_granted_value bigint NOT NULL,
    unit text NOT NULL,
    scale smallint NOT NULL CHECK (scale BETWEEN 0 AND 18),
    PRIMARY KEY (tenant_id, extension_id, dimension_id),
    FOREIGN KEY (tenant_id, extension_id) REFERENCES resource_extensions (tenant_id, id),
    FOREIGN KEY (tenant_id, dimension_id) REFERENCES resource_dimensions (tenant_id, id),
    CHECK (prior_granted_value <= 9223372036854775807 - added_value),
    CHECK (new_granted_value = prior_granted_value + added_value)
);

CREATE INDEX resource_extensions_tenant_run_created_idx ON resource_extensions (tenant_id, run_id, created_at, id);

CREATE TRIGGER resource_extensions_immutable
BEFORE UPDATE OR DELETE ON resource_extensions
FOR EACH ROW EXECUTE FUNCTION reject_agent_artifact_mutation();

CREATE TRIGGER resource_extension_items_immutable
BEFORE UPDATE OR DELETE ON resource_extension_items
FOR EACH ROW EXECUTE FUNCTION reject_agent_artifact_mutation();

---- create above / drop below ----

DROP TRIGGER resource_extension_items_immutable ON resource_extension_items;
DROP TRIGGER resource_extensions_immutable ON resource_extensions;
DROP TABLE resource_extension_items;
DROP TABLE resource_extensions;
