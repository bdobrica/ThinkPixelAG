package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/evidence"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.BreakGlassStore = (*TenantRepository)(nil)

func (r *TenantRepository) ActivateBreakGlass(ctx context.Context, grant domain.BreakGlassGrant, event evidence.Event) error {
	if err := r.valid(); err != nil {
		return err
	}
	if grant.TenantID != r.tenantID {
		return errors.New("break-glass grant does not match repository scope")
	}
	if err := grant.Validate(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return r.breakGlassTransaction(ctx, func(tx pgx.Tx) error {
		var inserted string
		err := tx.QueryRow(ctx, `WITH consumed AS (
INSERT INTO governance_approval_consumptions(approval_id,tenant_id,request_digest,consumed_at)
SELECT r.id,r.tenant_id,r.request_digest,$13 FROM governance_approval_requests r JOIN governance_approval_decisions d ON d.approval_id=r.id AND d.tenant_id=r.tenant_id
	WHERE r.tenant_id=$2 AND r.id=$4 AND r.request_digest=$9 AND r.requester_principal_id=$3 AND d.approved AND d.decided_at<r.expires_at AND d.decided_at<=$12 AND $12<r.expires_at AND r.action=CASE $5 WHEN 'POLICY_RECOVERY' THEN 'POLICY_BYPASS' ELSE 'GLOBAL_REVOCATION_CHANGE' END RETURNING approval_id),
	g AS (INSERT INTO break_glass_grants(id,tenant_id,principal_id,approval_id,scope,resource_type,resource_id,reason_code,grant_digest,credential_digest,strong_authentication_reference,issued_at,expires_at)
	SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13 FROM consumed RETURNING id),
	e AS (INSERT INTO break_glass_events(id,tenant_id,grant_id,actor_principal_id,change,occurred_at,event) SELECT $14,$2,id,$3,'ACTIVATED',$12,$15 FROM g RETURNING id)
	INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at)
	SELECT $14,$2,'break_glass',$1::text,'thinkpixelag.evidence/v1',1,$15,'{}'::jsonb,$12,$12 FROM e RETURNING aggregate_id`, grant.ID.String(), grant.TenantID.String(), grant.PrincipalID.String(), grant.ApprovalID.String(), string(grant.Scope), grant.ResourceType, grant.ResourceID, grant.ReasonCode, grant.GrantDigest, grant.CredentialDigest, grant.StrongAuthenticationReference, grant.IssuedAt, grant.ExpiresAt, event.ID.String(), mustJSON(event)).Scan(&inserted)
		if err != nil {
			return mapBreakGlassError("activate break-glass", err)
		}
		return nil
	})
}

func (r *TenantRepository) BreakGlass(ctx context.Context, tenantID, id domain.ID) (domain.BreakGlassGrant, error) {
	if err := r.valid(); err != nil {
		return domain.BreakGlassGrant{}, err
	}
	if tenantID != r.tenantID {
		return domain.BreakGlassGrant{}, domain.NewError(domain.CodeNotFound, "break-glass grant not found")
	}
	var g domain.BreakGlassGrant
	var ids [4]string
	err := r.db.QueryRow(ctx, `SELECT id::text,tenant_id::text,principal_id::text,approval_id::text,scope,resource_type,resource_id,reason_code,grant_digest,credential_digest,strong_authentication_reference,issued_at,expires_at FROM break_glass_grants WHERE tenant_id=$1 AND id=$2`, tenantID.String(), id.String()).Scan(&ids[0], &ids[1], &ids[2], &ids[3], &g.Scope, &g.ResourceType, &g.ResourceID, &g.ReasonCode, &g.GrantDigest, &g.CredentialDigest, &g.StrongAuthenticationReference, &g.IssuedAt, &g.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, domain.NewError(domain.CodeNotFound, "break-glass grant not found")
	}
	if err != nil {
		return g, fmt.Errorf("read break-glass grant: %w", err)
	}
	for i, raw := range ids {
		parsed, e := domain.ParseID(raw)
		if e != nil {
			return g, e
		}
		switch i {
		case 0:
			g.ID = parsed
		case 1:
			g.TenantID = parsed
		case 2:
			g.PrincipalID = parsed
		case 3:
			g.ApprovalID = parsed
		}
	}
	return g, g.Validate()
}

func (r *TenantRepository) UseBreakGlass(ctx context.Context, tenantID, principalID, id domain.ID, scope domain.BreakGlassScope, resourceType, resourceID, credentialDigest string, at time.Time, event evidence.Event) error {
	return r.appendBreakGlassEvent(ctx, tenantID, &principalID, id, "USED", at, event, `AND g.principal_id=$4 AND g.scope=$5 AND g.resource_type=$6 AND g.resource_id=$7 AND g.credential_digest=$8 AND g.issued_at<=$3 AND g.expires_at>$3 AND NOT EXISTS (SELECT 1 FROM break_glass_events t WHERE t.grant_id=g.id AND t.change IN ('EXPIRED','REVOKED'))`, principalID.String(), string(scope), resourceType, resourceID, credentialDigest)
}
func (r *TenantRepository) RevokeBreakGlass(ctx context.Context, tenantID, principalID, id domain.ID, at time.Time, event evidence.Event) error {
	return r.appendBreakGlassEvent(ctx, tenantID, &principalID, id, "REVOKED", at, event, `AND g.principal_id=$4 AND NOT EXISTS (SELECT 1 FROM break_glass_events t WHERE t.grant_id=g.id AND t.change IN ('EXPIRED','REVOKED'))`, principalID.String())
}
func (r *TenantRepository) ExpireBreakGlass(ctx context.Context, tenantID, id domain.ID, at time.Time, event evidence.Event) error {
	return r.appendBreakGlassEvent(ctx, tenantID, nil, id, "EXPIRED", at, event, `AND g.expires_at<=$3 AND NOT EXISTS (SELECT 1 FROM break_glass_events t WHERE t.grant_id=g.id AND t.change IN ('EXPIRED','REVOKED'))`)
}

func (r *TenantRepository) appendBreakGlassEvent(ctx context.Context, tenantID domain.ID, actor *domain.ID, id domain.ID, change string, at time.Time, event evidence.Event, predicate string, args ...any) error {
	if tenantID != r.tenantID {
		return domain.NewError(domain.CodeNotFound, "break-glass grant not found")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	base := []any{tenantID.String(), id.String(), at}
	base = append(base, args...)
	base = append(base, event.ID.String(), optionalDBID(actor), change, mustJSON(event))
	eventPos := len(base) - 3
	actorPos := len(base) - 2
	changePos := len(base) - 1
	jsonPos := len(base)
	query := fmt.Sprintf(`WITH g AS (SELECT id FROM break_glass_grants g WHERE g.tenant_id=$1 AND g.id=$2 %s FOR UPDATE), e AS (INSERT INTO break_glass_events(id,tenant_id,grant_id,actor_principal_id,change,occurred_at,event) SELECT $%d,$1,id,$%d,$%d,$3,$%d FROM g RETURNING id) INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) SELECT $%d,$1,'break_glass',$2::text,'thinkpixelag.evidence/v1',1,$%d,'{}'::jsonb,$3,$3 FROM e`, predicate, eventPos, actorPos, changePos, jsonPos, eventPos, jsonPos)
	return r.breakGlassTransaction(ctx, func(tx pgx.Tx) error {
		result, err := tx.Exec(ctx, query, base...)
		if err != nil {
			return mapBreakGlassError("append break-glass event", err)
		}
		if result.RowsAffected() != 1 {
			return domain.NewError(domain.CodeForbidden, "break-glass grant is expired, revoked, or does not match")
		}
		return nil
	})
}

func (r *TenantRepository) breakGlassTransaction(ctx context.Context, fn func(pgx.Tx) error) error {
	b, ok := r.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return errors.New("break-glass store requires transactions")
	}
	tx, err := b.Begin(ctx)
	if err != nil {
		return err
	}
	if err = fn(tx); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return err
	}
	return tx.Commit(ctx)
}
func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
func mapBreakGlassError(operation string, err error) error {
	switch ClassifyError(err) {
	case ErrorUniqueViolation:
		return domain.WrapError(domain.CodeConflict, "break-glass action was already established", err)
	case ErrorForeignKeyViolation, ErrorCheckViolation:
		return domain.WrapError(domain.CodeInvalidArgument, "break-glass references are invalid", err)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}
