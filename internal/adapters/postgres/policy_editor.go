package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/localapprovals"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ ports.PolicyEditorStore = (*TenantRepository)(nil)

func (r *TenantRepository) PolicyDraft(ctx context.Context, id domain.ID, revision int64) (ports.PolicyDraft, error) {
	var d ports.PolicyDraft
	d.ID = id
	err := r.db.QueryRow(ctx, `SELECT revision,content_digest,source,created_at FROM policy_draft_revisions WHERE tenant_id=$1 AND draft_id=$2 AND ($3=0 OR revision=$3) ORDER BY revision DESC LIMIT 1`, r.tenantID.String(), id.String(), revision).Scan(&d.Revision, &d.Digest, &d.Source, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, domain.NewError(domain.CodeNotFound, "draft revision not found")
	}
	d.CreatedAt = d.CreatedAt.UTC()
	return d, err
}
func (r *TenantRepository) SavePolicyDraft(ctx context.Context, op ports.AdministrationOperation, id domain.ID, expected int64, source, digest string) (ports.PolicyDraft, error) {
	raw, err := r.administrationTransaction(ctx, op, func(tx *TenantRepository) (any, error) {
		if id.IsZero() {
			var err error
			id, err = domain.NewID()
			if err != nil {
				return nil, err
			}
		}
		var current int64
		if err := tx.db.QueryRow(ctx, `SELECT COALESCE(max(revision),0) FROM policy_draft_revisions WHERE tenant_id=$1 AND draft_id=$2`, r.tenantID.String(), id.String()).Scan(&current); err != nil {
			return nil, err
		}
		if current != expected {
			return nil, domain.NewError(domain.CodeConflict, "draft revision changed")
		}
		d := ports.PolicyDraft{ID: id, Revision: current + 1, Digest: digest, Source: source, CreatedAt: op.At}
		_, err := tx.db.Exec(ctx, `INSERT INTO policy_draft_revisions(tenant_id,draft_id,revision,content_digest,source,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, r.tenantID.String(), id.String(), d.Revision, digest, source, op.ActorID.String(), op.At)
		d.Source = ""
		return d, err
	})
	var d ports.PolicyDraft
	if err == nil {
		err = json.Unmarshal(raw, &d)
	}
	return d, err
}
func (r *TenantRepository) ListPolicyDrafts(ctx context.Context, after domain.ID, limit int) ([]ports.PolicyDraft, error) {
	rows, err := r.db.Query(ctx, `SELECT DISTINCT ON(draft_id) draft_id::text,revision,content_digest,created_at FROM policy_draft_revisions WHERE tenant_id=$1 AND draft_id>$2 ORDER BY draft_id,revision DESC LIMIT $3`, r.tenantID.String(), after.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.PolicyDraft{}
	for rows.Next() {
		var d ports.PolicyDraft
		var id string
		if err := rows.Scan(&id, &d.Revision, &d.Digest, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.ID, err = domain.ParseID(id)
		if err != nil {
			return nil, err
		}
		d.CreatedAt = d.CreatedAt.UTC()
		out = append(out, d)
	}
	return out, rows.Err()
}
func (r *TenantRepository) ListPolicies(ctx context.Context, channel string, after domain.ID, limit int) ([]ports.PolicyArtifact, error) {
	rows, err := r.db.Query(ctx, `SELECT p.id::text,p.content_digest,p.contract_version,p.artifact_revision,CASE WHEN EXISTS(SELECT 1 FROM policy_activations a WHERE a.tenant_id=p.tenant_id AND a.policy_bundle_id=p.id AND a.deactivated_at IS NULL) THEN 'ACTIVE' ELSE p.validation_status END,p.created_at FROM policy_bundles p WHERE p.tenant_id=$1 AND p.channel=$2 AND p.id>$3 ORDER BY p.id LIMIT $4`, r.tenantID.String(), channel, after.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.PolicyArtifact{}
	for rows.Next() {
		var b ports.PolicyArtifact
		var id string
		if err := rows.Scan(&id, &b.Digest, &b.ContractVersion, &b.Revision, &b.State, &b.CreatedAt); err != nil {
			return nil, err
		}
		b.ID, err = domain.ParseID(id)
		if err != nil {
			return nil, err
		}
		b.CreatedAt = b.CreatedAt.UTC()
		out = append(out, b)
	}
	return out, rows.Err()
}
func (r *TenantRepository) ListPolicyActivations(ctx context.Context, channel string, after domain.ID, limit int) ([]ports.PolicyActivation, error) {
	rows, err := r.db.Query(ctx, `SELECT a.id::text,p.content_digest,a.channel,a.activation_version,a.activated_at FROM policy_activations a JOIN policy_bundles p ON p.tenant_id=a.tenant_id AND p.id=a.policy_bundle_id WHERE a.tenant_id=$1 AND a.channel=$2 AND a.id>$3 ORDER BY a.id LIMIT $4`, r.tenantID.String(), channel, after.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.PolicyActivation{}
	for rows.Next() {
		var a ports.PolicyActivation
		var id string
		if err := rows.Scan(&id, &a.Digest, &a.Channel, &a.Version, &a.ActivatedAt); err != nil {
			return nil, err
		}
		a.ID, err = domain.ParseID(id)
		if err != nil {
			return nil, err
		}
		a.ActivatedAt = a.ActivatedAt.UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}
func (r *TenantRepository) CreateLocalApproval(ctx context.Context, op ports.AdministrationOperation, a domain.GovernanceApproval) (ports.ApprovalView, error) {
	raw, err := r.administrationTransaction(ctx, op, func(tx *TenantRepository) (any, error) {
		if a.TenantID != r.tenantID || a.RequesterPrincipalID != op.ActorID {
			return nil, domain.NewError(domain.CodeForbidden, "approval scope mismatch")
		}
		return ports.ApprovalProjection(a, op.At), tx.CreateGovernanceApproval(ctx, a)
	})
	var v ports.ApprovalView
	if err == nil {
		err = json.Unmarshal(raw, &v)
	}
	return v, err
}
func (r *TenantRepository) LocalApprovalReceipt(ctx context.Context, id domain.ID) (ports.ApprovalAssertion, error) {
	a := ports.ApprovalAssertion{ApprovalID: id}
	var approver string
	err := r.db.QueryRow(ctx, `SELECT approver_principal_id::text,provider_reference,request_digest,approved,decision_reference,decided_at FROM local_approval_receipts WHERE tenant_id=$1 AND approval_id=$2`, r.tenantID.String(), id.String()).Scan(&approver, &a.ProviderReference, &a.RequestDigest, &a.Approved, &a.DecisionReference, &a.DecidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, domain.NewError(domain.CodeNotFound, "approval receipt not found")
	}
	if err != nil {
		return a, err
	}
	a.ApproverPrincipalID, err = domain.ParseID(approver)
	a.DecidedAt = a.DecidedAt.UTC()
	return a, err
}
func (r *TenantRepository) DecideLocalApproval(ctx context.Context, op ports.AdministrationOperation, id domain.ID, approved bool) (ports.ApprovalView, error) {
	raw, err := r.administrationTransaction(ctx, op, func(tx *TenantRepository) (any, error) {
		a, err := tx.GovernanceApproval(ctx, id)
		if err != nil {
			return nil, err
		}
		if a.Provider != "local-oidc" {
			return nil, domain.NewError(domain.CodeForbidden, "unsupported approval provider")
		}
		at := op.At.Truncate(time.Microsecond)
		reference := "local-decision:" + op.DecisionID.String()
		decided, err := a.Decide(op.ActorID, approved, reference, at)
		if err != nil {
			return nil, domain.NewError(domain.CodeConflict, "approval requires an independent operator and unexpired pending request")
		}
		_, err = tx.db.Exec(ctx, `INSERT INTO local_approval_receipts(tenant_id,approval_id,approver_principal_id,provider_reference,request_digest,approved,decision_reference,decided_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, r.tenantID.String(), id.String(), op.ActorID.String(), a.ProviderReference, a.RequestDigest, approved, reference, at)
		if err != nil {
			return nil, err
		}
		provider := &localapprovals.Provider{Store: tx}
		assertion := ports.ApprovalAssertion{ApprovalID: id, ProviderReference: a.ProviderReference, ApproverPrincipalID: op.ActorID, RequestDigest: a.RequestDigest, Approved: approved, DecisionReference: reference, DecidedAt: at}
		if err = provider.VerifyApproval(ctx, assertion); err != nil {
			return nil, err
		}
		if err = tx.RecordGovernanceApprovalDecision(ctx, decided); err != nil {
			return nil, err
		}
		return ports.ApprovalProjection(decided, at), nil
	})
	var v ports.ApprovalView
	if err == nil {
		err = json.Unmarshal(raw, &v)
	}
	return v, err
}
