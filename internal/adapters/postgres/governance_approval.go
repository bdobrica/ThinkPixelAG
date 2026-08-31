package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.GovernanceApprovalStore = (*TenantRepository)(nil)

func (r *TenantRepository) CreateGovernanceApproval(ctx context.Context, approval domain.GovernanceApproval) error {
	if err := r.valid(); err != nil {
		return err
	}
	if approval.TenantID != r.tenantID || approval.State != domain.GovernanceApprovalPending {
		return errors.New("governance approval does not match repository scope")
	}
	if err := approval.Validate(); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `INSERT INTO governance_approval_requests(id,tenant_id,requester_principal_id,action,resource_type,resource_id,request_digest,reason_code,provider,provider_reference,requested_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		approval.ID.String(), r.tenantID.String(), approval.RequesterPrincipalID.String(), string(approval.Action), approval.ResourceType, approval.ResourceID,
		approval.RequestDigest, approval.ReasonCode, approval.Provider, approval.ProviderReference, approval.RequestedAt, approval.ExpiresAt)
	if err != nil {
		return mapGovernanceApprovalWriteError("create governance approval", err)
	}
	return nil
}

func (r *TenantRepository) GovernanceApproval(ctx context.Context, id domain.ID) (domain.GovernanceApproval, error) {
	if err := r.valid(); err != nil {
		return domain.GovernanceApproval{}, err
	}
	return r.readGovernanceApproval(ctx, id, false)
}

func (r *TenantRepository) readGovernanceApproval(ctx context.Context, id domain.ID, lock bool) (domain.GovernanceApproval, error) {
	query := `SELECT r.id::text,r.tenant_id::text,r.requester_principal_id::text,r.action,r.resource_type,r.resource_id,r.request_digest,r.reason_code,r.provider,r.provider_reference,r.requested_at,r.expires_at,
d.approver_principal_id::text,d.approved,d.decision_reference,d.decided_at,c.consumed_at
FROM governance_approval_requests r LEFT JOIN governance_approval_decisions d ON d.tenant_id=r.tenant_id AND d.approval_id=r.id
LEFT JOIN governance_approval_consumptions c ON c.tenant_id=r.tenant_id AND c.approval_id=r.id WHERE r.tenant_id=$1 AND r.id=$2`
	if lock {
		query += ` FOR UPDATE OF r`
	}
	var approval domain.GovernanceApproval
	var idText, tenantText, requesterText string
	var approverText, decisionReference *string
	var approved *bool
	err := r.db.QueryRow(ctx, query, r.tenantID.String(), id.String()).Scan(&idText, &tenantText, &requesterText, &approval.Action,
		&approval.ResourceType, &approval.ResourceID, &approval.RequestDigest, &approval.ReasonCode, &approval.Provider, &approval.ProviderReference,
		&approval.RequestedAt, &approval.ExpiresAt, &approverText, &approved, &decisionReference, &approval.DecidedAt, &approval.ConsumedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.GovernanceApproval{}, domain.NewError(domain.CodeNotFound, "governance approval not found")
	}
	if err != nil {
		return domain.GovernanceApproval{}, fmt.Errorf("read governance approval: %w", err)
	}
	approval.ID, err = domain.ParseID(idText)
	if err != nil {
		return domain.GovernanceApproval{}, err
	}
	approval.TenantID, err = domain.ParseID(tenantText)
	if err != nil {
		return domain.GovernanceApproval{}, err
	}
	approval.RequesterPrincipalID, err = domain.ParseID(requesterText)
	if err != nil {
		return domain.GovernanceApproval{}, err
	}
	approval.State = domain.GovernanceApprovalPending
	if approved != nil {
		approval.State = domain.GovernanceApprovalRejected
		if *approved {
			approval.State = domain.GovernanceApprovalApproved
		}
		approval.DecisionReference = *decisionReference
		approval.ApproverPrincipalID, err = domain.ParseID(*approverText)
		if err != nil {
			return domain.GovernanceApproval{}, err
		}
	}
	if approval.ConsumedAt != nil {
		approval.State = domain.GovernanceApprovalConsumed
	}
	if err := approval.Validate(); err != nil {
		return domain.GovernanceApproval{}, fmt.Errorf("validate stored governance approval: %w", err)
	}
	return approval, nil
}

func (r *TenantRepository) RecordGovernanceApprovalDecision(ctx context.Context, approval domain.GovernanceApproval) error {
	if err := r.valid(); err != nil {
		return err
	}
	if approval.TenantID != r.tenantID || (approval.State != domain.GovernanceApprovalApproved && approval.State != domain.GovernanceApprovalRejected) {
		return errors.New("governance approval decision does not match repository scope")
	}
	if err := approval.Validate(); err != nil {
		return err
	}
	result, err := r.db.Exec(ctx, `INSERT INTO governance_approval_decisions(approval_id,tenant_id,requester_principal_id,approver_principal_id,approved,decision_reference,decided_at)
SELECT id,tenant_id,requester_principal_id,$3,$4,$5,$6 FROM governance_approval_requests WHERE tenant_id=$1 AND id=$2 AND requester_principal_id<>$3 AND expires_at>$6`,
		r.tenantID.String(), approval.ID.String(), approval.ApproverPrincipalID.String(), approval.State == domain.GovernanceApprovalApproved, approval.DecisionReference, *approval.DecidedAt)
	if err != nil {
		return mapGovernanceApprovalWriteError("record governance approval decision", err)
	}
	if result.RowsAffected() != 1 {
		return domain.NewError(domain.CodeConflict, "governance approval is expired or cannot be decided")
	}
	return nil
}

func (r *TenantRepository) ConsumeGovernanceApproval(ctx context.Context, id domain.ID, digest string, at time.Time) (domain.GovernanceApproval, error) {
	if err := r.valid(); err != nil {
		return domain.GovernanceApproval{}, err
	}
	beginner, ok := r.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return domain.GovernanceApproval{}, errors.New("governance approval consumption requires transactions")
	}
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return domain.GovernanceApproval{}, fmt.Errorf("begin governance approval consumption: %w", err)
	}
	txr := &TenantRepository{db: tx, tenantID: r.tenantID}
	approval, err := txr.readGovernanceApproval(ctx, id, true)
	if err == nil {
		_, err = approval.Consume(digest, at)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO governance_approval_consumptions(approval_id,tenant_id,request_digest,consumed_at) VALUES($1,$2,$3,$4)`, id.String(), r.tenantID.String(), digest, at)
	}
	if err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return domain.GovernanceApproval{}, mapGovernanceApprovalWriteError("consume governance approval", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.GovernanceApproval{}, fmt.Errorf("commit governance approval consumption: %w", err)
	}
	return approval.Consume(digest, at)
}

func mapGovernanceApprovalWriteError(operation string, err error) error {
	switch ClassifyError(err) {
	case ErrorUniqueViolation:
		return domain.WrapError(domain.CodeConflict, "governance approval already has an established result", err)
	case ErrorForeignKeyViolation, ErrorCheckViolation:
		return domain.WrapError(domain.CodeInvalidArgument, "governance approval references are invalid", err)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}
