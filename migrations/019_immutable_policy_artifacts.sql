-- Preserve exact reviewed source/signature metadata. Activation history may
-- close a current interval, but cannot rewrite which artifact was active.
CREATE FUNCTION reject_policy_artifact_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'policy artifacts are immutable'; END $$;
CREATE TRIGGER policy_artifacts_immutable BEFORE UPDATE OR DELETE ON policy_bundles
FOR EACH ROW EXECUTE FUNCTION reject_policy_artifact_mutation();
CREATE FUNCTION protect_policy_activation_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'policy activation history is immutable'; END IF;
 IF OLD.deactivated_at IS NOT NULL OR NEW.deactivated_at IS NULL
    OR NEW.deactivated_at < OLD.activated_at
    OR (to_jsonb(NEW)-'deactivated_at') IS DISTINCT FROM (to_jsonb(OLD)-'deactivated_at')
 THEN RAISE EXCEPTION 'only closing an active policy interval is permitted'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER policy_activation_history_immutable BEFORE UPDATE OR DELETE ON policy_activations
FOR EACH ROW EXECUTE FUNCTION protect_policy_activation_history();

---- create above / drop below ----
DROP TRIGGER policy_activation_history_immutable ON policy_activations;
DROP FUNCTION protect_policy_activation_history();
DROP TRIGGER policy_artifacts_immutable ON policy_bundles;
DROP FUNCTION reject_policy_artifact_mutation();
