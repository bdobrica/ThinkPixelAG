package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/jackc/pgx/v5"
)

type PolicyStore struct {
	db        DBTX
	tx        *Transactor
	freshness *policy.Freshness
}

func NewPolicyStore(db DBTX, tx *Transactor, f *policy.Freshness) (*PolicyStore, error) {
	if db == nil || tx == nil || f == nil {
		return nil, errors.New("policy store dependencies are required")
	}
	return &PolicyStore{db, tx, f}, nil
}

type PolicyBundle struct {
	ID, TenantID, CreatedBy                       domain.ID
	Channel, Digest, ContractVersion, SignerKeyID string
	Bundle, Signature                             []byte
	ValidFrom, ValidUntil                         *time.Time
	CreatedAt                                     time.Time
}

func (s *PolicyStore) VerifyAndPersist(ctx context.Context, b PolicyBundle, verifier policy.SignatureVerifier, validator policy.BundleValidator) error {
	if err := policy.VerifyBundle(ctx, b.Digest, b.SignerKeyID, b.Bundle, b.Signature, verifier, validator); err != nil {
		return err
	}
	return s.persistVerified(ctx, b)
}

func (s *PolicyStore) persistVerified(ctx context.Context, b PolicyBundle) error {
	if b.ID.IsZero() || b.TenantID.IsZero() || b.CreatedBy.IsZero() || b.Channel == "" || b.ContractVersion != policy.ContractVersion || b.Digest == "" || len(b.Bundle) == 0 || len(b.Signature) == 0 || b.SignerKeyID == "" {
		return errors.New("invalid verified policy bundle")
	}
	_, err := s.db.Exec(ctx, `INSERT INTO policy_bundles (id,tenant_id,channel,content_digest,contract_version,bundle,signature,signer_key_id,validation_status,valid_from,valid_until,created_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'VALIDATED',$9,$10,$11,$12)`, b.ID.String(), b.TenantID.String(), b.Channel, b.Digest, b.ContractVersion, b.Bundle, b.Signature, b.SignerKeyID, b.ValidFrom, b.ValidUntil, b.CreatedBy.String(), b.CreatedAt)
	if err != nil {
		return fmt.Errorf("persist policy bundle: %w", err)
	}
	return nil
}
func (s *PolicyStore) Activate(ctx context.Context, tenant, bundle, actor domain.ID, channel, reason string, now time.Time) (policy.ActiveBundle, error) {
	var active policy.ActiveBundle
	err := s.tx.WithinTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable}, func(ctx context.Context, tx DBTX) error {
		var digest, status string
		var from, until *time.Time
		if err := tx.QueryRow(ctx, `SELECT content_digest,validation_status,valid_from,valid_until FROM policy_bundles WHERE tenant_id=$1 AND id=$2 AND channel=$3 FOR UPDATE`, tenant.String(), bundle.String(), channel).Scan(&digest, &status, &from, &until); err != nil {
			return fmt.Errorf("select policy bundle: %w", err)
		}
		if status != "VALIDATED" && status != "APPROVED" {
			return errors.New("policy bundle is not validated")
		}
		if from != nil && now.Before(*from) || until != nil && !now.Before(*until) {
			return errors.New("policy bundle is outside validity window")
		}
		if _, err := tx.Exec(ctx, `UPDATE policy_activations SET deactivated_at=$3 WHERE tenant_id=$1 AND channel=$2 AND deactivated_at IS NULL`, tenant.String(), channel, now); err != nil {
			return err
		}
		var version int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(activation_version),0)+1 FROM policy_activations WHERE tenant_id=$1 AND channel=$2`, tenant.String(), channel).Scan(&version); err != nil {
			return err
		}
		id, err := domain.NewID()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO policy_activations (id,tenant_id,channel,policy_bundle_id,activation_version,actor_principal_id,reason_code,activated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, id.String(), tenant.String(), channel, bundle.String(), version, actor.String(), reason, now); err != nil {
			return err
		}
		active = policy.ActiveBundle{TenantID: tenant.String(), Channel: channel, BundleID: bundle.String(), Digest: digest, Version: version, ActivatedAt: now, RefreshedAt: now}
		return nil
	})
	if err != nil {
		return policy.ActiveBundle{}, err
	}
	if err := s.freshness.Set(active); err != nil {
		return policy.ActiveBundle{}, fmt.Errorf("update local policy freshness: %w", err)
	}
	return active, nil
}

func (s *PolicyStore) RefreshActive(ctx context.Context, tenant domain.ID, channel string, observedAt time.Time) (policy.ActiveBundle, error) {
	var a policy.ActiveBundle
	a.TenantID, a.Channel, a.RefreshedAt = tenant.String(), channel, observedAt
	err := s.db.QueryRow(ctx, `SELECT pa.policy_bundle_id,pb.content_digest,pa.activation_version,pa.activated_at FROM policy_activations pa JOIN policy_bundles pb ON pb.tenant_id=pa.tenant_id AND pb.id=pa.policy_bundle_id WHERE pa.tenant_id=$1 AND pa.channel=$2 AND pa.deactivated_at IS NULL`, tenant.String(), channel).Scan(&a.BundleID, &a.Digest, &a.Version, &a.ActivatedAt)
	if err != nil {
		return policy.ActiveBundle{}, fmt.Errorf("refresh active policy: %w", err)
	}
	if err := s.freshness.Set(a); err != nil {
		return policy.ActiveBundle{}, err
	}
	return a, nil
}
func (s *PolicyStore) Rollback(ctx context.Context, tenant, previousBundle, actor domain.ID, channel, reason string, now time.Time) (policy.ActiveBundle, error) {
	return s.Activate(ctx, tenant, previousBundle, actor, channel, reason, now)
}
