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

var _ ports.RevocationDistributionRepository = (*Repositories)(nil)
var _ ports.RevocationAuthority = (*Repositories)(nil)

func (r *Repositories) AuthoritativeRevocations(ctx context.Context, tenant, agent domain.ID, now time.Time) (ports.RevocationAuthorityState, error) {
	s, err := r.RevocationSnapshot(ctx, tenant, now)
	if err == nil && !agent.IsZero() {
		err = r.db.QueryRow(ctx, `SELECT COALESCE((SELECT revocation_epoch FROM agent_security_epochs WHERE tenant_id=$1 AND agent_id=$2),0)`, tenant.String(), agent.String()).Scan(&s.Epochs.AgentRevocation)
	}
	return ports.RevocationAuthorityState{Epochs: s.Epochs, Active: s.Active}, err
}

func (r *Repositories) RevocationChanges(ctx context.Context, tenant domain.ID, after int64, limit int, retainedAfter time.Time) ([]ports.RevocationLogEntry, error) {
	if r == nil || r.db == nil || tenant.IsZero() || after < 0 || limit < 1 || limit > 1000 || retainedAfter.IsZero() {
		return nil, errors.New("revocation change query is invalid")
	}
	var earliest *int64
	var hasExpired bool
	if err := r.db.QueryRow(ctx, `SELECT min(sequence) FILTER (WHERE committed_at >= $2),COALESCE(bool_or(committed_at < $2),false) FROM revocation_log WHERE tenant_id=$1 OR tenant_id IS NULL`, tenant.String(), retainedAfter).Scan(&earliest, &hasExpired); err != nil {
		return nil, fmt.Errorf("read revocation retention boundary: %w", err)
	}
	if hasExpired && (earliest == nil || after < *earliest-1) {
		return nil, ports.ErrRevocationCursorGone
	}
	rows, err := r.db.Query(ctx, `SELECT l.event_id::text,l.sequence,l.revocation_id::text,l.tenant_id::text,l.scope,l.target,l.change_type,l.security_epoch,COALESCE(l.tenant_policy_epoch,0),COALESCE(l.tenant_revocation_epoch,0),COALESCE(l.agent_revocation_epoch,0),l.committed_at,r.reason_code,COALESCE(r.detail_reference,''),COALESCE(r.approval_reference,''),r.effective_at,r.expires_at,r.created_at,COALESCE(r.actor_principal_id::text,'') FROM revocation_log l JOIN revocations r ON r.id=l.revocation_id WHERE (l.tenant_id=$1 OR l.tenant_id IS NULL) AND l.sequence>$2 AND l.committed_at >= $3 ORDER BY l.sequence LIMIT $4`, tenant.String(), after, retainedAfter, limit)
	if err != nil {
		return nil, fmt.Errorf("query revocation changes: %w", err)
	}
	defer rows.Close()
	return scanRevocationLog(rows)
}

func scanRevocationLog(rows pgx.Rows) ([]ports.RevocationLogEntry, error) {
	out := []ports.RevocationLogEntry{}
	for rows.Next() {
		var event, rid, scope, target, change, reason, detail, approval, actor string
		var tenant *string
		var expires *time.Time
		var e ports.RevocationLogEntry
		if err := rows.Scan(&event, &e.Sequence, &rid, &tenant, &scope, &target, &change, &e.Epochs.Security, &e.Epochs.TenantPolicy, &e.Epochs.TenantRevocation, &e.Epochs.AgentRevocation, &e.OccurredAt, &reason, &detail, &approval, &e.Revocation.EffectiveAt, &expires, &e.Revocation.CreatedAt, &actor); err != nil {
			return nil, err
		}
		e.EventID, _ = domain.ParseID(event)
		e.Revocation.ID, _ = domain.ParseID(rid)
		e.Revocation.Scope = domain.RevocationScope(scope)
		e.Revocation.Target = target
		e.Revocation.ReasonCode = reason
		e.Revocation.DetailReference = detail
		e.Revocation.ApprovalReference = approval
		e.Revocation.ExpiresAt = expires
		e.Change = domain.RevocationChangeType(change)
		if tenant != nil {
			id, _ := domain.ParseID(*tenant)
			e.Revocation.TenantID = &id
		}
		if actor != "" {
			e.Revocation.ActorPrincipalID, _ = domain.ParseID(actor)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repositories) RevocationSnapshot(ctx context.Context, tenant domain.ID, now time.Time) (ports.RevocationSnapshot, error) {
	var out ports.RevocationSnapshot
	if r == nil || r.db == nil || tenant.IsZero() || now.IsZero() {
		return out, errors.New("revocation snapshot query is invalid")
	}
	if err := r.db.QueryRow(ctx, `SELECT COALESCE(max(sequence),0),(SELECT security_epoch FROM security_epochs WHERE singleton),COALESCE((SELECT policy_epoch FROM tenant_security_epochs WHERE tenant_id=$1),0),COALESCE((SELECT revocation_epoch FROM tenant_security_epochs WHERE tenant_id=$1),0),COALESCE((SELECT max(revocation_epoch) FROM agent_security_epochs WHERE tenant_id=$1),0) FROM revocation_log WHERE tenant_id=$1 OR tenant_id IS NULL`, tenant.String()).Scan(&out.Sequence, &out.Epochs.Security, &out.Epochs.TenantPolicy, &out.Epochs.TenantRevocation, &out.Epochs.AgentRevocation); err != nil {
		return out, err
	}
	rows, err := r.db.Query(ctx, `SELECT r.id::text,r.tenant_id::text,r.scope,r.target,r.reason_code,COALESCE(r.detail_reference,''),COALESCE(r.approval_reference,''),r.effective_at,r.expires_at,r.created_at,COALESCE(r.actor_principal_id::text,'') FROM revocations r WHERE (r.tenant_id=$1 OR r.tenant_id IS NULL) AND r.effective_at<=$2 AND (r.expires_at IS NULL OR r.expires_at>$2) AND (SELECT change_type FROM revocation_changes c WHERE c.revocation_id=r.id ORDER BY c.changed_at DESC,c.id DESC LIMIT 1)='CREATED' ORDER BY r.scope,r.target,r.id`, tenant.String(), now)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v domain.Revocation
		var id, scope, actor string
		var tid *string
		if err := rows.Scan(&id, &tid, &scope, &v.Target, &v.ReasonCode, &v.DetailReference, &v.ApprovalReference, &v.EffectiveAt, &v.ExpiresAt, &v.CreatedAt, &actor); err != nil {
			return out, err
		}
		v.ID, _ = domain.ParseID(id)
		v.Scope = domain.RevocationScope(scope)
		if tid != nil {
			x, _ := domain.ParseID(*tid)
			v.TenantID = &x
		}
		if actor != "" {
			v.ActorPrincipalID, _ = domain.ParseID(actor)
		}
		out.Active = append(out.Active, v)
	}
	return out, rows.Err()
}

func (r *Repositories) SaveGatewayCheckpoint(ctx context.Context, c ports.GatewayCheckpoint) error {
	if r == nil || r.db == nil || c.TenantID.IsZero() || c.GatewayPrincipalID.IsZero() || c.LastSequence < 0 || c.Epochs.Security < 0 || c.Epochs.TenantPolicy < 0 || c.Epochs.TenantRevocation < 0 || c.UpdatedAt.IsZero() {
		return errors.New("gateway checkpoint is invalid")
	}
	tag, err := r.db.Exec(ctx, `INSERT INTO gateway_checkpoints(tenant_id,gateway_principal_id,last_sequence,security_epoch,tenant_policy_epoch,tenant_revocation_epoch,last_stream_received_at,last_reconciled_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(tenant_id,gateway_principal_id) DO UPDATE SET last_sequence=EXCLUDED.last_sequence,security_epoch=EXCLUDED.security_epoch,tenant_policy_epoch=EXCLUDED.tenant_policy_epoch,tenant_revocation_epoch=EXCLUDED.tenant_revocation_epoch,last_stream_received_at=COALESCE(EXCLUDED.last_stream_received_at,gateway_checkpoints.last_stream_received_at),last_reconciled_at=COALESCE(EXCLUDED.last_reconciled_at,gateway_checkpoints.last_reconciled_at),updated_at=EXCLUDED.updated_at WHERE gateway_checkpoints.last_sequence<=EXCLUDED.last_sequence AND gateway_checkpoints.security_epoch<=EXCLUDED.security_epoch AND gateway_checkpoints.tenant_policy_epoch<=EXCLUDED.tenant_policy_epoch AND gateway_checkpoints.tenant_revocation_epoch<=EXCLUDED.tenant_revocation_epoch`, c.TenantID.String(), c.GatewayPrincipalID.String(), c.LastSequence, c.Epochs.Security, c.Epochs.TenantPolicy, c.Epochs.TenantRevocation, c.LastStreamReceivedAt, c.LastReconciledAt, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save gateway checkpoint: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.NewError(domain.CodeConflict, "gateway checkpoint cannot regress")
	}
	return nil
}
