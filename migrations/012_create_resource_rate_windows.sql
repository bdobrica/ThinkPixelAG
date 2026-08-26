CREATE TABLE resource_rate_windows (
    tenant_id uuid NOT NULL,
    envelope_id uuid NOT NULL,
    dimension_id uuid NOT NULL,
    window_start timestamptz NOT NULL,
    used_value bigint NOT NULL CHECK (used_value >= 0),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, envelope_id, dimension_id, window_start),
    FOREIGN KEY (tenant_id, envelope_id, dimension_id)
        REFERENCES resource_envelope_grants (tenant_id, envelope_id, dimension_id),
    CHECK (window_start = date_trunc('minute', window_start))
);

CREATE INDEX resource_rate_windows_expiry_idx
    ON resource_rate_windows (window_start, tenant_id, envelope_id);

---- create above / drop below ----

DROP TABLE resource_rate_windows;
