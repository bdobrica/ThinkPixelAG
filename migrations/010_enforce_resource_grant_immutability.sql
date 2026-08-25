CREATE TRIGGER resource_envelope_grants_immutable
BEFORE UPDATE OR DELETE ON resource_envelope_grants
FOR EACH ROW EXECUTE FUNCTION reject_agent_artifact_mutation();

---- create above / drop below ----

DROP TRIGGER resource_envelope_grants_immutable ON resource_envelope_grants;
