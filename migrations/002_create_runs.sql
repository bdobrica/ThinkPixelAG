CREATE TABLE runs (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    parent_run_id uuid,
    requested_by uuid NOT NULL,
    state text NOT NULL CHECK (state IN (
        'PENDING', 'REJECTED', 'ADMITTED', 'RUNNING', 'COMPLETED', 'FAILED',
        'CANCELLED', 'TIMED_OUT', 'BUDGET_EXHAUSTED', 'PAUSED_FOR_BUDGET', 'FAILED_BUDGET'
    )),
    state_version bigint NOT NULL DEFAULT 1 CHECK (state_version > 0),
    constraints jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(constraints) = 'object'),
    deadline_at timestamptz,
    lease_id uuid,
    lease_expires_at timestamptz,
    fencing_token bigint NOT NULL DEFAULT 0 CHECK (fencing_token >= 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    terminal_at timestamptz,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_id, agent_version_id)
        REFERENCES agent_versions (tenant_id, agent_id, id),
    FOREIGN KEY (tenant_id, parent_run_id) REFERENCES runs (tenant_id, id),
    FOREIGN KEY (tenant_id, requested_by) REFERENCES principals (tenant_id, id),
    CHECK (parent_run_id IS NULL OR parent_run_id <> id),
    CHECK ((lease_id IS NULL) = (lease_expires_at IS NULL)),
    CHECK (updated_at >= created_at),
    CHECK (terminal_at IS NULL OR terminal_at >= created_at),
    CHECK (
        (state IN ('REJECTED', 'COMPLETED', 'FAILED', 'CANCELLED', 'TIMED_OUT', 'FAILED_BUDGET'))
        = (terminal_at IS NOT NULL)
    )
);

CREATE INDEX runs_tenant_state_created_idx ON runs (tenant_id, state, created_at, id);
CREATE INDEX runs_tenant_agent_created_idx ON runs (tenant_id, agent_id, created_at DESC, id);
CREATE INDEX runs_tenant_parent_idx ON runs (tenant_id, parent_run_id, id) WHERE parent_run_id IS NOT NULL;
CREATE INDEX runs_claimable_idx ON runs (tenant_id, state, lease_expires_at, id)
    WHERE state IN ('ADMITTED', 'RUNNING');

CREATE TABLE run_version_resolutions (
    run_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    agent_content_digest text NOT NULL CHECK (agent_content_digest ~ '^sha256:[0-9a-f]{64}$'),
    policy_bundle_digest text NOT NULL CHECK (policy_bundle_digest ~ '^sha256:[0-9a-f]{64}$'),
    policy_activation_version bigint NOT NULL CHECK (policy_activation_version > 0),
    approval_id uuid NOT NULL,
    resolution jsonb NOT NULL CHECK (jsonb_typeof(resolution) = 'object'),
    resolved_at timestamptz NOT NULL,
    UNIQUE (tenant_id, run_id),
    FOREIGN KEY (tenant_id, run_id) REFERENCES runs (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_id, agent_version_id)
        REFERENCES agent_versions (tenant_id, agent_id, id),
    FOREIGN KEY (tenant_id, approval_id) REFERENCES agent_version_approvals (tenant_id, id)
);

CREATE INDEX run_version_resolutions_tenant_version_idx
    ON run_version_resolutions (tenant_id, agent_version_id, resolved_at DESC, run_id);

CREATE TABLE run_signals (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    run_id uuid NOT NULL,
    signal_type text NOT NULL CHECK (signal_type IN ('CANCEL', 'PAUSE', 'RESUME', 'CUSTOM')),
    payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 256),
    actor_principal_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (tenant_id, run_id, idempotency_key),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, run_id) REFERENCES runs (tenant_id, id),
    FOREIGN KEY (tenant_id, actor_principal_id) REFERENCES principals (tenant_id, id)
);

CREATE INDEX run_signals_tenant_run_created_idx
    ON run_signals (tenant_id, run_id, created_at, id);

CREATE TABLE run_events (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    run_id uuid NOT NULL,
    sequence bigint NOT NULL CHECK (sequence > 0),
    event_type text NOT NULL CHECK (length(event_type) BETWEEN 1 AND 128),
    actor_type text NOT NULL CHECK (actor_type IN ('CALLER', 'WORKER', 'GOVERNOR', 'OPERATOR', 'SYSTEM')),
    actor_id uuid,
    state text CHECK (state IS NULL OR state IN (
        'PENDING', 'REJECTED', 'ADMITTED', 'RUNNING', 'COMPLETED', 'FAILED',
        'CANCELLED', 'TIMED_OUT', 'BUDGET_EXHAUSTED', 'PAUSED_FOR_BUDGET', 'FAILED_BUDGET'
    )),
    state_version bigint CHECK (state_version IS NULL OR state_version > 0),
    payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    occurred_at timestamptz NOT NULL,
    UNIQUE (tenant_id, run_id, sequence),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, run_id) REFERENCES runs (tenant_id, id)
);

CREATE INDEX run_events_tenant_run_sequence_idx
    ON run_events (tenant_id, run_id, sequence);

---- create above / drop below ----

DROP TABLE run_events;
DROP TABLE run_signals;
DROP TABLE run_version_resolutions;
DROP TABLE runs;
