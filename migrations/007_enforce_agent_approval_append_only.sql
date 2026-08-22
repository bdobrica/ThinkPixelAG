CREATE TRIGGER agent_version_approvals_append_only
    BEFORE UPDATE OR DELETE ON agent_version_approvals
    FOR EACH ROW EXECUTE FUNCTION reject_agent_artifact_mutation();

---- create above / drop below ----

DROP TRIGGER agent_version_approvals_append_only ON agent_version_approvals;
