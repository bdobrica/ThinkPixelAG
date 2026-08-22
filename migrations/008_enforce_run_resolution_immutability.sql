CREATE TRIGGER run_version_resolutions_immutable
    BEFORE UPDATE OR DELETE ON run_version_resolutions
    FOR EACH ROW EXECUTE FUNCTION reject_agent_artifact_mutation();

---- create above / drop below ----

DROP TRIGGER run_version_resolutions_immutable ON run_version_resolutions;
