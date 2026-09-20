CREATE TABLE operator_bootstrap_receipts (
 tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
 request_digest text NOT NULL CHECK(request_digest ~ '^sha256:[0-9a-f]{64}$'),
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL
);
CREATE TRIGGER operator_bootstrap_receipts_immutable BEFORE UPDATE OR DELETE ON operator_bootstrap_receipts
FOR EACH ROW EXECUTE FUNCTION reject_policy_artifact_mutation();

---- create above / drop below ----
DROP TABLE operator_bootstrap_receipts;
