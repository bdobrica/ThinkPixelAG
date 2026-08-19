CREATE TABLE idempotency_records (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    principal_id uuid NOT NULL,
    route text NOT NULL CHECK (length(route) BETWEEN 1 AND 256),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 256),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    state text NOT NULL CHECK (state IN ('IN_PROGRESS', 'COMPLETED', 'FAILED')),
    response_status integer CHECK (response_status IS NULL OR response_status BETWEEN 100 AND 599),
    response_headers jsonb CHECK (response_headers IS NULL OR jsonb_typeof(response_headers) = 'object'),
    response_body bytea,
    owner_token uuid NOT NULL,
    locked_until timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    completed_at timestamptz,
    expires_at timestamptz NOT NULL,
    UNIQUE (tenant_id, principal_id, route, idempotency_key),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, principal_id) REFERENCES principals (tenant_id, id),
    CHECK (expires_at > created_at),
    CHECK ((state = 'COMPLETED') = (completed_at IS NOT NULL)),
    CHECK ((state = 'COMPLETED') = (response_status IS NOT NULL)),
    CHECK (completed_at IS NULL OR completed_at >= created_at)
);

CREATE INDEX idempotency_records_expiry_idx ON idempotency_records (expires_at, tenant_id, id);
CREATE INDEX idempotency_records_in_progress_idx
    ON idempotency_records (locked_until, tenant_id, id) WHERE state = 'IN_PROGRESS';

CREATE TABLE audit_events (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    id uuid NOT NULL UNIQUE,
    tenant_id uuid,
    principal_id uuid,
    action text NOT NULL CHECK (length(action) BETWEEN 1 AND 128),
    resource_type text NOT NULL CHECK (length(resource_type) BETWEEN 1 AND 128),
    resource_id text CHECK (resource_id IS NULL OR length(resource_id) <= 512),
    outcome text NOT NULL CHECK (outcome IN ('SUCCEEDED', 'DENIED', 'FAILED')),
    reason_codes jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(reason_codes) = 'array'),
    policy_decision_id uuid,
    request_id uuid,
    trace_id text CHECK (trace_id IS NULL OR trace_id ~ '^[0-9a-f]{32}$'),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    previous_event_hash text CHECK (previous_event_hash IS NULL OR previous_event_hash ~ '^sha256:[0-9a-f]{64}$'),
    event_hash text NOT NULL CHECK (event_hash ~ '^sha256:[0-9a-f]{64}$'),
    occurred_at timestamptz NOT NULL,
    FOREIGN KEY (tenant_id, principal_id) REFERENCES principals (tenant_id, id)
);

CREATE INDEX audit_events_tenant_sequence_idx ON audit_events (tenant_id, sequence);
CREATE INDEX audit_events_tenant_resource_idx
    ON audit_events (tenant_id, resource_type, resource_id, occurred_at DESC, sequence);

CREATE TABLE outbox_messages (
    id uuid PRIMARY KEY,
    tenant_id uuid,
    aggregate_type text NOT NULL CHECK (length(aggregate_type) BETWEEN 1 AND 128),
    aggregate_id text NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 512),
    event_type text NOT NULL CHECK (length(event_type) BETWEEN 1 AND 256),
    schema_version integer NOT NULL CHECK (schema_version > 0),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    headers jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(headers) = 'object'),
    occurred_at timestamptz NOT NULL,
    available_at timestamptz NOT NULL,
    claimed_by text CHECK (claimed_by IS NULL OR length(claimed_by) <= 256),
    claim_token uuid,
    claimed_until timestamptz,
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_attempt_at timestamptz,
    last_error_code text CHECK (last_error_code IS NULL OR length(last_error_code) <= 128),
    published_at timestamptz,
    dead_lettered_at timestamptz,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    CHECK ((claimed_by IS NULL) = (claim_token IS NULL)),
    CHECK ((claim_token IS NULL) = (claimed_until IS NULL)),
    CHECK (published_at IS NULL OR published_at >= occurred_at),
    CHECK (dead_lettered_at IS NULL OR dead_lettered_at >= occurred_at),
    CHECK (NOT (published_at IS NOT NULL AND dead_lettered_at IS NOT NULL))
);

CREATE INDEX outbox_messages_ready_idx
    ON outbox_messages (available_at, occurred_at, id)
    WHERE published_at IS NULL AND dead_lettered_at IS NULL;
CREATE INDEX outbox_messages_claim_expiry_idx
    ON outbox_messages (claimed_until, id)
    WHERE published_at IS NULL AND dead_lettered_at IS NULL AND claimed_until IS NOT NULL;
CREATE INDEX outbox_messages_tenant_aggregate_idx
    ON outbox_messages (tenant_id, aggregate_type, aggregate_id, occurred_at, id);

---- create above / drop below ----

DROP TABLE outbox_messages;
DROP TABLE audit_events;
DROP TABLE idempotency_records;
