CREATE TABLE role_mapping_revisions (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 issuer text NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 mappings jsonb NOT NULL CHECK(jsonb_typeof(mappings)='object'),
 created_by uuid NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,issuer,revision),
 FOREIGN KEY(tenant_id,created_by) REFERENCES principals(tenant_id,id)
);
CREATE TRIGGER role_mapping_revisions_immutable BEFORE UPDATE OR DELETE ON role_mapping_revisions
FOR EACH ROW EXECUTE FUNCTION reject_policy_artifact_mutation();

---- create above / drop below ----
DROP TABLE role_mapping_revisions;
