ALTER TABLE resource_settlements
    ADD COLUMN policy_decision_id uuid NOT NULL,
    ADD COLUMN idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 256),
    ADD COLUMN terminal_run_state text NOT NULL CHECK (terminal_run_state IN ('COMPLETED','FAILED','CANCELLED','TIMED_OUT','FAILED_BUDGET')),
    ADD COLUMN content_digest text NOT NULL CHECK (content_digest ~ '^sha256:[0-9a-f]{64}$'),
    ADD CONSTRAINT resource_settlements_actor_idempotency_unique UNIQUE (tenant_id, actor_principal_id, idempotency_key);

CREATE TRIGGER resource_settlements_immutable BEFORE UPDATE OR DELETE ON resource_settlements
FOR EACH ROW EXECUTE FUNCTION reject_agent_artifact_mutation();
CREATE TRIGGER resource_settlement_items_immutable BEFORE UPDATE OR DELETE ON resource_settlement_items
FOR EACH ROW EXECUTE FUNCTION reject_agent_artifact_mutation();

---- create above / drop below ----

DROP TRIGGER resource_settlement_items_immutable ON resource_settlement_items;
DROP TRIGGER resource_settlements_immutable ON resource_settlements;
ALTER TABLE resource_settlements
    DROP CONSTRAINT resource_settlements_actor_idempotency_unique,
    DROP COLUMN content_digest, DROP COLUMN terminal_run_state,
    DROP COLUMN idempotency_key, DROP COLUMN policy_decision_id;
