CREATE TABLE policy_draft_revisions (
 tenant_id uuid NOT NULL REFERENCES tenants(id),
 draft_id uuid NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 content_digest text NOT NULL CHECK(content_digest ~ '^sha256:[0-9a-f]{64}$'),
 source text NOT NULL CHECK(octet_length(source) BETWEEN 1 AND 1048576),
 created_by uuid NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,draft_id,revision),
 FOREIGN KEY(tenant_id,created_by) REFERENCES principals(tenant_id,id)
);
CREATE TABLE local_approval_receipts (
 tenant_id uuid NOT NULL,
 approval_id uuid NOT NULL,
 approver_principal_id uuid NOT NULL,
 provider_reference text NOT NULL,
 request_digest text NOT NULL,
 approved boolean NOT NULL,
 decision_reference text NOT NULL,
 decided_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,approval_id),
 FOREIGN KEY(tenant_id,approval_id,request_digest) REFERENCES governance_approval_requests(tenant_id,id,request_digest),
 FOREIGN KEY(tenant_id,approver_principal_id) REFERENCES principals(tenant_id,id)
);
CREATE TRIGGER policy_drafts_immutable BEFORE UPDATE OR DELETE ON policy_draft_revisions
FOR EACH ROW EXECUTE FUNCTION reject_policy_artifact_mutation();
CREATE TRIGGER local_approval_receipts_immutable BEFORE UPDATE OR DELETE ON local_approval_receipts
FOR EACH ROW EXECUTE FUNCTION reject_policy_artifact_mutation();

---- create above / drop below ----
DROP TABLE local_approval_receipts;
DROP TABLE policy_draft_revisions;
