package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ ports.PolicyAdministrationStore = (*TenantRepository)(nil)

func (r *TenantRepository) PolicyArtifact(ctx context.Context, channel, digest string) (ports.PolicyArtifact, error) {
	var b ports.PolicyArtifact
	var id string
	err := r.db.QueryRow(ctx, `SELECT id::text,content_digest,contract_version,artifact_revision,bundle,signature,signer_key_id,signer_key_version,signature_algorithm,channel,validation_status,created_at FROM policy_bundles WHERE tenant_id=$1 AND channel=$2 AND content_digest=$3`, r.tenantID.String(), channel, digest).Scan(&id, &b.Digest, &b.ContractVersion, &b.Revision, &b.Source, &b.Signature.Value, &b.Signature.KeyID, &b.Signature.KeyVersion, &b.Signature.Algorithm, &b.Channel, &b.State, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, domain.NewError(domain.CodeNotFound, "policy not found")
	}
	if err != nil {
		return b, err
	}
	b.ID, err = domain.ParseID(id)
	b.CreatedAt = b.CreatedAt.UTC()
	return b, err
}
func (r *TenantRepository) CurrentPolicy(ctx context.Context, channel string) (ports.PolicyArtifact, ports.PolicyActivation, error) {
	var a ports.PolicyActivation
	var id string
	err := r.db.QueryRow(ctx, `SELECT pa.id::text,pb.content_digest,pa.channel,pa.activation_version,pa.activated_at FROM policy_activations pa JOIN policy_bundles pb ON pb.tenant_id=pa.tenant_id AND pb.id=pa.policy_bundle_id WHERE pa.tenant_id=$1 AND pa.channel=$2 AND pa.deactivated_at IS NULL AND pb.validation_status IN ('VALIDATED','APPROVED') AND (pb.valid_from IS NULL OR pb.valid_from<=now()) AND (pb.valid_until IS NULL OR pb.valid_until>now())`, r.tenantID.String(), channel).Scan(&id, &a.Digest, &a.Channel, &a.Version, &a.ActivatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.PolicyArtifact{}, a, domain.NewError(domain.CodeUnavailable, "no active policy")
	}
	if err != nil {
		return ports.PolicyArtifact{}, a, err
	}
	a.ID, err = domain.ParseID(id)
	if err != nil {
		return ports.PolicyArtifact{}, a, err
	}
	a.ActivatedAt = a.ActivatedAt.UTC()
	b, err := r.PolicyArtifact(ctx, channel, a.Digest)
	return b, a, err
}

// administrationTransaction commits the mutation, evidence and replay response
// together. Tenant locking serializes first activation and configuration writes;
// policy binding is rechecked inside this transaction after authorization.
func (r *TenantRepository) administrationTransaction(ctx context.Context, op ports.AdministrationOperation, fn func(*TenantRepository) (any, error)) (json.RawMessage, error) {
	if op.TenantID != r.tenantID || op.ActorID.IsZero() || op.DecisionID.IsZero() {
		return nil, domain.NewError(domain.CodeForbidden, "invalid administration scope")
	}
	var result json.RawMessage
	err := r.withAdmissionTransaction(ctx, func(tx *TenantRepository) error {
		if _, err := tx.db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, r.tenantID.String()); err != nil {
			return err
		}
		acquisition, err := tx.AcquireIdempotency(ctx, ports.IdempotencyRequest{PrincipalID: op.ActorID, Route: op.Action + ":" + op.Resource, Key: op.Key, RequestHash: op.RequestHash, Lease: time.Minute, TTL: 24 * time.Hour}, op.At)
		if errors.Is(err, ErrIdempotencyConflict) || errors.Is(err, ErrIdempotencyInFlight) {
			return domain.NewError(domain.CodeConflict, "idempotency conflict")
		}
		if err != nil {
			return err
		}
		if acquisition.Outcome == ports.IdempotencyReplay {
			result = acquisition.Response.Body
			return nil
		}
		_, active, err := tx.CurrentPolicy(ctx, op.Channel)
		if err != nil {
			return err
		}
		if active.Digest != op.PolicyDigest || active.Version != op.PolicyVersion {
			return domain.NewError(domain.CodeConflict, "policy changed; authorize again")
		}
		response, err := fn(tx)
		if err != nil {
			return err
		}
		result, err = json.Marshal(response)
		if err != nil {
			return err
		}
		if err := tx.administrationEvidence(ctx, op); err != nil {
			return err
		}
		return tx.CompleteIdempotency(ctx, acquisition, ports.IdempotencyResponse{Status: 201, Headers: json.RawMessage(`{"Content-Type":["application/json"]}`), Body: result}, op.At)
	})
	return result, err
}
func (r *TenantRepository) administrationEvidence(ctx context.Context, op ports.AdministrationOperation) error {
	audit, err := domain.NewID()
	if err != nil {
		return err
	}
	outbox, err := domain.NewID()
	if err != nil {
		return err
	}
	metadata, _ := json.Marshal(map[string]any{"resource_reference": op.Resource, "policy_digest": op.PolicyDigest, "policy_epoch": op.PolicyVersion})
	_, err = r.db.Exec(ctx, `INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,reason_codes,policy_decision_id,request_id,metadata,event_hash,occurred_at) VALUES($1,$2,$3,$4,'governance_configuration',$5,'SUCCEEDED','["governance.operation.allowed"]',$6,$7,$8,$9,$10)`, audit.String(), r.tenantID.String(), op.ActorID.String(), op.Action, op.Resource, op.DecisionID.String(), op.RequestID.String(), metadata, op.RequestHash, op.At)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx, `INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) VALUES($1,$2,'governance_configuration',$3,'governance.configuration.changed',1,$4,'{}',$5,$5)`, outbox.String(), r.tenantID.String(), op.Resource, metadata, op.At)
	return err
}
func (r *TenantRepository) StorePolicy(ctx context.Context, op ports.AdministrationOperation, b ports.PolicyArtifact) (ports.PolicyArtifact, error) {
	raw, err := r.administrationTransaction(ctx, op, func(tx *TenantRepository) (any, error) {
		existing, err := tx.PolicyArtifact(ctx, b.Channel, b.Digest)
		if err == nil {
			if existing.Revision != b.Revision || existing.Signature.KeyID != b.Signature.KeyID || existing.Signature.KeyVersion != b.Signature.KeyVersion {
				return nil, domain.NewError(domain.CodeConflict, "artifact digest already has different signature metadata")
			}
			return existing, nil
		}
		if domain.ErrorCodeOf(err) != domain.CodeNotFound {
			return nil, err
		}
		store := &PolicyStore{db: tx.db}
		err = store.persistVerified(ctx, PolicyBundle{ID: b.ID, TenantID: r.tenantID, CreatedBy: op.ActorID, Channel: b.Channel, Digest: b.Digest, ContractVersion: b.ContractVersion, ArtifactRevision: b.Revision, Bundle: b.Source, Signature: b.Signature.Value, SignerKeyID: b.Signature.KeyID, SignerKeyVersion: b.Signature.KeyVersion, SignatureAlgorithm: b.Signature.Algorithm, CreatedAt: op.At})
		return b, err
	})
	var result ports.PolicyArtifact
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}
func (r *TenantRepository) ActivatePolicy(ctx context.Context, op ports.AdministrationOperation, reason, approvalRef string) (ports.PolicyActivation, error) {
	raw, err := r.administrationTransaction(ctx, op, func(tx *TenantRepository) (any, error) {
		target, err := tx.PolicyArtifact(ctx, op.Channel, op.Resource)
		if err != nil {
			return nil, err
		}
		if target.State != "VALIDATED" && target.State != "APPROVED" {
			return nil, domain.NewError(domain.CodeConflict, "policy is not validated")
		}
		var valid bool
		if err := tx.db.QueryRow(ctx, `SELECT (valid_from IS NULL OR valid_from<=$3) AND (valid_until IS NULL OR valid_until>$3) FROM policy_bundles WHERE tenant_id=$1 AND id=$2`, r.tenantID.String(), target.ID.String(), op.At).Scan(&valid); err != nil {
			return nil, err
		}
		if !valid {
			return nil, domain.NewError(domain.CodeConflict, "policy outside validity window")
		}
		var rollback bool
		if err := tx.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM policy_activations WHERE tenant_id=$1 AND policy_bundle_id=$2)`, r.tenantID.String(), target.ID.String()).Scan(&rollback); err != nil {
			return nil, err
		}
		if rollback {
			approvalID, err := domain.ParseID(approvalRef)
			if err != nil {
				return nil, domain.NewError(domain.CodeForbidden, "rollback requires a bound approval")
			}
			approval, err := tx.GovernanceApproval(ctx, approvalID)
			if err != nil {
				return nil, err
			}
			if approval.Action != domain.ApprovalPolicyRollback || approval.RequesterPrincipalID != op.ActorID {
				return nil, domain.NewError(domain.CodeForbidden, "rollback approval has wrong action or requester")
			}
			if _, err := tx.ConsumeGovernanceApproval(ctx, approvalID, domain.PolicyActivationDigest(r.tenantID, op.Channel, target.Digest, op.PolicyVersion), op.At); err != nil {
				return nil, err
			}
		} else if approvalRef != "not-required" {
			return nil, domain.NewError(domain.CodeInvalidArgument, "initial activation uses approval_reference not-required")
		}
		if _, err := tx.db.Exec(ctx, `UPDATE policy_activations SET deactivated_at=$3 WHERE tenant_id=$1 AND channel=$2 AND deactivated_at IS NULL`, r.tenantID.String(), op.Channel, op.At); err != nil {
			return nil, err
		}
		var version int64
		if err := tx.db.QueryRow(ctx, `SELECT COALESCE(max(activation_version),0)+1 FROM policy_activations WHERE tenant_id=$1 AND channel=$2`, r.tenantID.String(), op.Channel).Scan(&version); err != nil {
			return nil, err
		}
		id, err := domain.NewID()
		if err != nil {
			return nil, err
		}
		if _, err := tx.db.Exec(ctx, `INSERT INTO policy_activations(id,tenant_id,channel,policy_bundle_id,activation_version,actor_principal_id,policy_decision_id,reason_code,activated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id.String(), r.tenantID.String(), op.Channel, target.ID.String(), version, op.ActorID.String(), op.DecisionID.String(), reason, op.At); err != nil {
			return nil, err
		}
		_, err = tx.db.Exec(ctx, `INSERT INTO tenant_security_epochs(tenant_id,policy_epoch,revocation_epoch) VALUES($1,1,0) ON CONFLICT(tenant_id) DO UPDATE SET policy_epoch=tenant_security_epochs.policy_epoch+1`, r.tenantID.String())
		return ports.PolicyActivation{ID: id, Digest: target.Digest, Channel: op.Channel, Version: version, ActivatedAt: op.At}, err
	})
	var result ports.PolicyActivation
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}
