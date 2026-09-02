\set ON_ERROR_STOP on
DO $$ BEGIN
  IF (SELECT count(*) FROM thinkpixelag_schema_version) <> 1 THEN
    RAISE EXCEPTION 'schema version row is missing or duplicated';
  END IF;
  IF EXISTS (SELECT 1 FROM security_epochs WHERE security_epoch < 0) OR
     EXISTS (SELECT 1 FROM tenant_security_epochs WHERE policy_epoch < 0 OR revocation_epoch < 0) OR
     EXISTS (SELECT 1 FROM agent_security_epochs WHERE revocation_epoch < 0) THEN
    RAISE EXCEPTION 'negative security epoch';
  END IF;
  IF EXISTS (SELECT 1 FROM gateway_checkpoints WHERE last_sequence < 0 OR security_epoch < 0 OR tenant_policy_epoch < 0 OR tenant_revocation_epoch < 0) THEN
    RAISE EXCEPTION 'invalid gateway checkpoint';
  END IF;
  IF EXISTS (
    SELECT 1 FROM resource_balances b
    JOIN resource_envelope_grants g USING (tenant_id,envelope_id,dimension_id)
    WHERE b.available_value + b.direct_consumed_value + b.allocated_open_value >
      g.granted_value + COALESCE((SELECT sum(i.added_value) FROM resource_extension_items i JOIN resource_extensions x ON x.tenant_id=i.tenant_id AND x.id=i.extension_id WHERE x.tenant_id=b.tenant_id AND x.envelope_id=b.envelope_id AND i.dimension_id=b.dimension_id),0)
  ) THEN
    RAISE EXCEPTION 'resource allocation exceeds grant';
  END IF;
  IF EXISTS (SELECT 1 FROM evidence_delivery_receipts r LEFT JOIN outbox_messages o ON o.id=r.event_id WHERE o.id IS NULL) THEN
    RAISE EXCEPTION 'evidence receipt has no outbox source';
  END IF;
END $$;
