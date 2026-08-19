CREATE TABLE resource_dimensions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (name ~ '^[a-z][a-z0-9_]{0,62}$'),
    class text NOT NULL CHECK (class IN ('CONSUMABLE', 'STRUCTURAL', 'DEADLINE')),
    unit text NOT NULL CHECK (length(unit) BETWEEN 1 AND 64),
    scale smallint NOT NULL DEFAULT 0 CHECK (scale BETWEEN 0 AND 18),
    minimum_value bigint NOT NULL DEFAULT 0 CHECK (minimum_value >= 0),
    maximum_value bigint NOT NULL CHECK (maximum_value >= 0),
    aggregation text NOT NULL CHECK (aggregation IN ('SUM', 'MAX', 'MIN', 'ABSOLUTE')),
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, name),
    UNIQUE (tenant_id, id),
    CHECK (maximum_value >= minimum_value),
    CHECK ((class = 'CONSUMABLE' AND aggregation = 'SUM') OR class <> 'CONSUMABLE'),
    CHECK ((class = 'DEADLINE' AND aggregation = 'ABSOLUTE') OR class <> 'DEADLINE')
);

CREATE INDEX resource_dimensions_tenant_class_idx
    ON resource_dimensions (tenant_id, class, id);

CREATE TABLE resource_envelopes (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    run_id uuid NOT NULL,
    parent_envelope_id uuid,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    issued_by uuid NOT NULL,
    policy_decision_id uuid NOT NULL,
    issued_at timestamptz NOT NULL,
    UNIQUE (tenant_id, run_id),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, run_id) REFERENCES runs (tenant_id, id),
    FOREIGN KEY (tenant_id, parent_envelope_id) REFERENCES resource_envelopes (tenant_id, id),
    FOREIGN KEY (tenant_id, issued_by) REFERENCES principals (tenant_id, id),
    CHECK (parent_envelope_id IS NULL OR parent_envelope_id <> id)
);

CREATE INDEX resource_envelopes_tenant_parent_idx
    ON resource_envelopes (tenant_id, parent_envelope_id, id) WHERE parent_envelope_id IS NOT NULL;

CREATE TABLE resource_envelope_grants (
    tenant_id uuid NOT NULL,
    envelope_id uuid NOT NULL,
    dimension_id uuid NOT NULL,
    granted_value bigint NOT NULL CHECK (granted_value >= 0),
    unit text NOT NULL CHECK (length(unit) BETWEEN 1 AND 64),
    scale smallint NOT NULL CHECK (scale BETWEEN 0 AND 18),
    PRIMARY KEY (tenant_id, envelope_id, dimension_id),
    FOREIGN KEY (tenant_id, envelope_id) REFERENCES resource_envelopes (tenant_id, id),
    FOREIGN KEY (tenant_id, dimension_id) REFERENCES resource_dimensions (tenant_id, id)
);

CREATE TABLE resource_balances (
    tenant_id uuid NOT NULL,
    envelope_id uuid NOT NULL,
    dimension_id uuid NOT NULL,
    available_value bigint NOT NULL CHECK (available_value >= 0),
    direct_consumed_value bigint NOT NULL DEFAULT 0 CHECK (direct_consumed_value >= 0),
    allocated_open_value bigint NOT NULL DEFAULT 0 CHECK (allocated_open_value >= 0),
    state_version bigint NOT NULL DEFAULT 1 CHECK (state_version > 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, envelope_id, dimension_id),
    FOREIGN KEY (tenant_id, envelope_id, dimension_id)
        REFERENCES resource_envelope_grants (tenant_id, envelope_id, dimension_id),
    CHECK (available_value <= 9223372036854775807 - direct_consumed_value),
    CHECK (available_value + direct_consumed_value <= 9223372036854775807 - allocated_open_value)
);

CREATE TABLE resource_reservations (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    parent_envelope_id uuid NOT NULL,
    child_envelope_id uuid NOT NULL,
    child_run_id uuid NOT NULL,
    state text NOT NULL CHECK (state IN ('OPEN', 'SETTLED', 'EXPIRED_SETTLED', 'CANCELLED_SETTLED')),
    expires_at timestamptz,
    created_at timestamptz NOT NULL,
    settled_at timestamptz,
    UNIQUE (tenant_id, child_envelope_id),
    UNIQUE (tenant_id, child_run_id),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, parent_envelope_id) REFERENCES resource_envelopes (tenant_id, id),
    FOREIGN KEY (tenant_id, child_envelope_id) REFERENCES resource_envelopes (tenant_id, id),
    FOREIGN KEY (tenant_id, child_run_id) REFERENCES runs (tenant_id, id),
    CHECK (parent_envelope_id <> child_envelope_id),
    CHECK ((state = 'OPEN') = (settled_at IS NULL)),
    CHECK (settled_at IS NULL OR settled_at >= created_at),
    CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE INDEX resource_reservations_tenant_parent_state_idx
    ON resource_reservations (tenant_id, parent_envelope_id, state, id);
CREATE INDEX resource_reservations_open_expiry_idx
    ON resource_reservations (expires_at, tenant_id, id) WHERE state = 'OPEN';

CREATE TABLE resource_reservation_items (
    tenant_id uuid NOT NULL,
    reservation_id uuid NOT NULL,
    dimension_id uuid NOT NULL,
    reserved_value bigint NOT NULL CHECK (reserved_value >= 0),
    unit text NOT NULL CHECK (length(unit) BETWEEN 1 AND 64),
    scale smallint NOT NULL CHECK (scale BETWEEN 0 AND 18),
    PRIMARY KEY (tenant_id, reservation_id, dimension_id),
    FOREIGN KEY (tenant_id, reservation_id) REFERENCES resource_reservations (tenant_id, id),
    FOREIGN KEY (tenant_id, dimension_id) REFERENCES resource_dimensions (tenant_id, id)
);

CREATE TABLE trusted_usage_entries (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    run_id uuid NOT NULL,
    envelope_id uuid NOT NULL,
    dimension_id uuid NOT NULL,
    producer_id uuid NOT NULL,
    source_event_id text NOT NULL CHECK (length(source_event_id) BETWEEN 1 AND 256),
    quantity_value bigint NOT NULL CHECK (quantity_value >= 0),
    unit text NOT NULL CHECK (length(unit) BETWEEN 1 AND 64),
    scale smallint NOT NULL CHECK (scale BETWEEN 0 AND 18),
    content_digest text NOT NULL CHECK (content_digest ~ '^sha256:[0-9a-f]{64}$'),
    observed_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    UNIQUE (tenant_id, producer_id, source_event_id),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, run_id) REFERENCES runs (tenant_id, id),
    FOREIGN KEY (tenant_id, envelope_id) REFERENCES resource_envelopes (tenant_id, id),
    FOREIGN KEY (tenant_id, dimension_id) REFERENCES resource_dimensions (tenant_id, id),
    FOREIGN KEY (tenant_id, producer_id) REFERENCES principals (tenant_id, id)
);

CREATE INDEX trusted_usage_entries_tenant_run_dimension_idx
    ON trusted_usage_entries (tenant_id, run_id, dimension_id, recorded_at, id);

CREATE TABLE resource_settlements (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    reservation_id uuid NOT NULL,
    actor_principal_id uuid NOT NULL,
    reason text NOT NULL CHECK (reason IN ('TERMINAL', 'EXPIRED', 'CANCELLED', 'RECONCILED')),
    settled_at timestamptz NOT NULL,
    UNIQUE (tenant_id, reservation_id),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, reservation_id) REFERENCES resource_reservations (tenant_id, id),
    FOREIGN KEY (tenant_id, actor_principal_id) REFERENCES principals (tenant_id, id)
);

CREATE TABLE resource_settlement_items (
    tenant_id uuid NOT NULL,
    settlement_id uuid NOT NULL,
    dimension_id uuid NOT NULL,
    reserved_value bigint NOT NULL CHECK (reserved_value >= 0),
    consumed_value bigint NOT NULL CHECK (consumed_value >= 0),
    returned_value bigint NOT NULL CHECK (returned_value >= 0),
    unit text NOT NULL CHECK (length(unit) BETWEEN 1 AND 64),
    scale smallint NOT NULL CHECK (scale BETWEEN 0 AND 18),
    PRIMARY KEY (tenant_id, settlement_id, dimension_id),
    FOREIGN KEY (tenant_id, settlement_id) REFERENCES resource_settlements (tenant_id, id),
    FOREIGN KEY (tenant_id, dimension_id) REFERENCES resource_dimensions (tenant_id, id),
    CHECK (consumed_value <= reserved_value),
    CHECK (returned_value = reserved_value - consumed_value)
);

---- create above / drop below ----

DROP TABLE resource_settlement_items;
DROP TABLE resource_settlements;
DROP TABLE trusted_usage_entries;
DROP TABLE resource_reservation_items;
DROP TABLE resource_reservations;
DROP TABLE resource_balances;
DROP TABLE resource_envelope_grants;
DROP TABLE resource_envelopes;
DROP TABLE resource_dimensions;
