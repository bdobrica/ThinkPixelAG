CREATE TABLE integration_revisions (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 integration text NOT NULL CHECK(integration='opa'),
 revision bigint NOT NULL CHECK(revision>0),
 connection jsonb NOT NULL CHECK(jsonb_typeof(connection)='object'),
 created_by uuid NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,integration,revision),
 FOREIGN KEY(tenant_id,created_by) REFERENCES principals(tenant_id,id)
);
CREATE TRIGGER integration_revisions_immutable BEFORE UPDATE OR DELETE ON integration_revisions
FOR EACH ROW EXECUTE FUNCTION reject_policy_artifact_mutation();

---- create above / drop below ----
DROP TABLE integration_revisions;
