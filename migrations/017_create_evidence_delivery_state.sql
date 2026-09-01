CREATE TABLE evidence_sink_checkpoints (
    sink_id text PRIMARY KEY CHECK (length(sink_id) BETWEEN 1 AND 256),
    last_sequence bigint NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    last_event_hash text CHECK (last_event_hash IS NULL OR last_event_hash ~ '^sha256:[0-9a-f]{64}$'),
    claim_token uuid,
    claimed_event_id uuid REFERENCES outbox_messages(id),
    claimed_until timestamptz,
    updated_at timestamptz NOT NULL,
    CHECK ((last_sequence = 0) = (last_event_hash IS NULL)),
    CHECK ((claim_token IS NULL) = (claimed_event_id IS NULL) AND (claim_token IS NULL) = (claimed_until IS NULL))
);

CREATE TABLE evidence_delivery_receipts (
    sink_id text NOT NULL REFERENCES evidence_sink_checkpoints(sink_id),
    event_id uuid NOT NULL REFERENCES outbox_messages(id),
    sequence bigint NOT NULL CHECK (sequence > 0),
    previous_event_hash text CHECK (previous_event_hash IS NULL OR previous_event_hash ~ '^sha256:[0-9a-f]{64}$'),
    event_hash text NOT NULL CHECK (event_hash ~ '^sha256:[0-9a-f]{64}$'),
    receipt_id text NOT NULL CHECK (length(receipt_id) BETWEEN 1 AND 512),
    sink_checkpoint text NOT NULL CHECK (sink_checkpoint ~ '^sha256:[0-9a-f]{64}$'),
    accepted_at timestamptz NOT NULL,
    receipt jsonb NOT NULL CHECK (jsonb_typeof(receipt) = 'object'),
    PRIMARY KEY (sink_id, event_id),
    UNIQUE (sink_id, sequence),
    CHECK ((sequence = 1) = (previous_event_hash IS NULL))
);

CREATE FUNCTION reject_evidence_receipt_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'evidence delivery receipts are append-only';
END $$;

CREATE TRIGGER evidence_delivery_receipts_immutable
BEFORE UPDATE OR DELETE ON evidence_delivery_receipts
FOR EACH ROW EXECUTE FUNCTION reject_evidence_receipt_mutation();

---- create above / drop below ----
DROP TRIGGER evidence_delivery_receipts_immutable ON evidence_delivery_receipts;
DROP FUNCTION reject_evidence_receipt_mutation();
DROP TABLE evidence_delivery_receipts;
DROP TABLE evidence_sink_checkpoints;
