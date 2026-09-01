ALTER TABLE policy_bundles
    ADD COLUMN artifact_revision bigint,
    ADD COLUMN signer_key_version text,
    ADD COLUMN signature_algorithm text;

-- Previously validated rows predate the signed-envelope contract and cannot
-- be activated without re-promotion. Preserve them as immutable evidence.
UPDATE policy_bundles
SET validation_status = 'REJECTED'
WHERE validation_status IN ('VALIDATED', 'APPROVED');

ALTER TABLE policy_bundles
    ADD CONSTRAINT policy_bundles_artifact_revision_valid
        CHECK (artifact_revision IS NULL OR artifact_revision > 0),
    ADD CONSTRAINT policy_bundles_signer_key_version_valid
        CHECK (signer_key_version IS NULL OR length(signer_key_version) BETWEEN 1 AND 256),
    ADD CONSTRAINT policy_bundles_signature_algorithm_valid
        CHECK (signature_algorithm IS NULL OR signature_algorithm IN ('ED25519', 'ECDSA_SHA256', 'RSA_PSS_SHA256')),
    ADD CONSTRAINT policy_bundles_signed_metadata_complete
        CHECK ((artifact_revision IS NULL) = (signer_key_version IS NULL)
           AND (artifact_revision IS NULL) = (signature_algorithm IS NULL)),
    ADD CONSTRAINT policy_bundles_artifact_revision_unique
        UNIQUE (tenant_id, channel, artifact_revision);

---- create above / drop below ----

ALTER TABLE policy_bundles
    DROP CONSTRAINT policy_bundles_artifact_revision_unique,
    DROP CONSTRAINT policy_bundles_signed_metadata_complete,
    DROP CONSTRAINT policy_bundles_signature_algorithm_valid,
    DROP CONSTRAINT policy_bundles_signer_key_version_valid,
    DROP CONSTRAINT policy_bundles_artifact_revision_valid,
    DROP COLUMN signature_algorithm,
    DROP COLUMN signer_key_version,
    DROP COLUMN artifact_revision;
