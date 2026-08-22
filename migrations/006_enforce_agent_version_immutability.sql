ALTER TABLE agent_capabilities
    DROP CONSTRAINT agent_capabilities_capability_identifier_check;

ALTER TABLE agent_capabilities
    ADD CONSTRAINT agent_capabilities_capability_identifier_check
    CHECK (length(capability_identifier) BETWEEN 1 AND 1024);

CREATE FUNCTION reject_agent_artifact_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER agent_versions_immutable
    BEFORE UPDATE OR DELETE ON agent_versions
    FOR EACH ROW EXECUTE FUNCTION reject_agent_artifact_mutation();

CREATE TRIGGER agent_capabilities_immutable
    BEFORE UPDATE OR DELETE ON agent_capabilities
    FOR EACH ROW EXECUTE FUNCTION reject_agent_artifact_mutation();

---- create above / drop below ----

DROP TRIGGER agent_capabilities_immutable ON agent_capabilities;
DROP TRIGGER agent_versions_immutable ON agent_versions;
DROP FUNCTION reject_agent_artifact_mutation();

ALTER TABLE agent_capabilities
    DROP CONSTRAINT agent_capabilities_capability_identifier_check;

ALTER TABLE agent_capabilities
    ADD CONSTRAINT agent_capabilities_capability_identifier_check
    CHECK (length(capability_identifier) BETWEEN 1 AND 512);
