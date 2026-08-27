package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.RevocationRepository = (*TenantRepository)(nil)

func (r *TenantRepository) CreateRevocation(ctx context.Context, v domain.Revocation, e ports.RevocationEvidence) (out domain.RevocationResult, err error) {
	v, err = domain.ValidateRevocation(v)
	if err != nil {
		return out, err
	}
	if v.Scope != domain.RevocationGlobal && (v.TenantID == nil || *v.TenantID != r.tenantID) {
		return out, errors.New("revocation does not match repository scope")
	}
	return r.changeRevocation(ctx, v, nil, e)
}
func (r *TenantRepository) LiftRevocation(ctx context.Context, v domain.RevocationLift, e ports.RevocationEvidence) (out domain.RevocationResult, err error) {
	v, err = domain.ValidateRevocationLift(v)
	if err != nil {
		return out, err
	}
	if v.TenantID != nil && *v.TenantID != r.tenantID {
		return out, errors.New("revocation lift does not match repository scope")
	}
	return r.changeRevocation(ctx, domain.Revocation{}, &v, e)
}

func (r *TenantRepository) changeRevocation(ctx context.Context, created domain.Revocation, lift *domain.RevocationLift, e ports.RevocationEvidence) (out domain.RevocationResult, err error) {
	if err := r.valid(); err != nil {
		return out, err
	}
	if e.ChangeID.IsZero() || e.EventID.IsZero() || e.AuditID.IsZero() || e.OutboxID.IsZero() || e.RequestID.IsZero() || e.PolicyDecisionID.IsZero() || len(e.ReasonCodes) == 0 {
		return out, errors.New("revocation evidence is invalid")
	}
	err = r.withAdmissionTransaction(ctx, func(txr *TenantRepository) error {
		v := created
		change := domain.RevocationCreated
		at := created.CreatedAt
		changeReason := created.ReasonCode
		actor := created.ActorPrincipalID
		if lift != nil {
			change = domain.RevocationLifted
			at = lift.ChangedAt
			changeReason = lift.ReasonCode
			actor = lift.ActorPrincipalID
			var tid *string
			var scope, target, reason, detail, approval, actorID string
			var effective, createdAt time.Time
			var expires *time.Time
			if err := txr.db.QueryRow(ctx, `SELECT tenant_id::text,scope,target,reason_code,COALESCE(detail_reference,''),COALESCE(actor_principal_id::text,''),COALESCE(approval_reference,''),effective_at,expires_at,created_at FROM revocations WHERE id=$1 FOR UPDATE`, lift.RevocationID.String()).Scan(&tid, &scope, &target, &reason, &detail, &actorID, &approval, &effective, &expires, &createdAt); errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError(domain.CodeNotFound, "revocation not found")
			} else if err != nil {
				return fmt.Errorf("lock revocation: %w", err)
			}
			if tid != nil && *tid != r.tenantID.String() {
				return domain.NewError(domain.CodeNotFound, "revocation not found")
			}
			var last string
			if err := txr.db.QueryRow(ctx, `SELECT change_type FROM revocation_changes WHERE revocation_id=$1 ORDER BY changed_at DESC,id DESC LIMIT 1`, lift.RevocationID.String()).Scan(&last); err != nil {
				return fmt.Errorf("read revocation state: %w", err)
			}
			if last != "CREATED" {
				return domain.NewError(domain.CodeConflict, "revocation is not active")
			}
			id, _ := domain.ParseID(lift.RevocationID.String())
			v = domain.Revocation{ID: id, ActorPrincipalID: actor, Scope: domain.RevocationScope(scope), Target: target, ReasonCode: reason, DetailReference: detail, ApprovalReference: approval, EffectiveAt: effective, ExpiresAt: expires, CreatedAt: createdAt}
			if tid != nil {
				t, _ := domain.ParseID(*tid)
				v.TenantID = &t
			}
		} else {
			_, err := txr.db.Exec(ctx, `INSERT INTO revocations(id,tenant_id,scope,target,reason_code,detail_reference,actor_principal_id,approval_reference,effective_at,expires_at,created_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,NULLIF($8,''),$9,$10,$11)`, v.ID.String(), optionalDBID(v.TenantID), string(v.Scope), v.Target, v.ReasonCode, v.DetailReference, v.ActorPrincipalID.String(), v.ApprovalReference, v.EffectiveAt, v.ExpiresAt, v.CreatedAt)
			if err != nil {
				return fmt.Errorf("insert revocation: %w", err)
			}
		}
		if _, err := txr.db.Exec(ctx, `INSERT INTO revocation_changes(id,revocation_id,tenant_id,change_type,actor_principal_id,reason_code,changed_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, e.ChangeID.String(), v.ID.String(), optionalDBID(v.TenantID), string(change), actor.String(), changeReason, at); err != nil {
			return fmt.Errorf("append revocation change: %w", err)
		}
		epochs, err := txr.incrementRevocationEpochs(ctx, v)
		if err != nil {
			return err
		}
		var seq int64
		if err := txr.db.QueryRow(ctx, `INSERT INTO revocation_log(event_id,revocation_id,change_id,tenant_id,scope,target,change_type,security_epoch,tenant_policy_epoch,tenant_revocation_epoch,agent_revocation_epoch,committed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING sequence`, e.EventID.String(), v.ID.String(), e.ChangeID.String(), optionalDBID(v.TenantID), string(v.Scope), v.Target, string(change), epochs.Security, nullableEpoch(v.TenantID, epochs.TenantPolicy), nullableEpoch(v.TenantID, epochs.TenantRevocation), nullableAgentEpoch(epochs.AgentRevocation), at).Scan(&seq); err != nil {
			return fmt.Errorf("append revocation log: %w", err)
		}
		reasons, _ := json.Marshal(e.ReasonCodes)
		payload, _ := json.Marshal(map[string]any{"event_id": e.EventID.String(), "sequence": seq, "revocation_id": v.ID.String(), "change": change, "scope": v.Scope, "target": v.Target, "epochs": epochs})
		metadata, _ := json.Marshal(map[string]any{"scope": v.Scope, "target": v.Target, "change": change, "reason_code": changeReason, "approval_reference": func() string {
			if lift != nil {
				return lift.ApprovalReference
			}
			return v.ApprovalReference
		}()})
		eventHash, err := hashAuditEvent(AuditEvent{ID: e.AuditID, TenantID: &r.tenantID, PrincipalID: &actor, Action: "revocations." + strings.ToLower(string(change)), ResourceType: "revocation", ResourceID: v.ID.String(), Outcome: "SUCCEEDED", ReasonCodes: reasons, PolicyDecisionID: &e.PolicyDecisionID, RequestID: &e.RequestID, Metadata: metadata, OccurredAt: at})
		if err != nil {
			return err
		}
		if _, err := txr.db.Exec(ctx, `WITH a AS (INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,reason_codes,policy_decision_id,request_id,metadata,event_hash,occurred_at) VALUES($1,$2,$3,$4,'revocation',$5,'SUCCEEDED',$6::jsonb,$7,$8,$9::jsonb,$10,$11)) INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) VALUES($12,$13,'revocation',$5,$14,1,$15::jsonb,'{}'::jsonb,$11,$11)`, e.AuditID.String(), r.tenantID.String(), actor.String(), "revocations."+strings.ToLower(string(change)), v.ID.String(), reasons, e.PolicyDecisionID.String(), e.RequestID.String(), metadata, eventHash, at, e.OutboxID.String(), optionalDBID(v.TenantID), "revocation."+strings.ToLower(string(change)), payload); err != nil {
			return fmt.Errorf("append revocation evidence: %w", err)
		}
		out = domain.RevocationResult{RevocationID: v.ID, Revocation: v, State: change, Sequence: seq, Epochs: epochs}
		return nil
	})
	return out, err
}

func (r *TenantRepository) incrementRevocationEpochs(ctx context.Context, v domain.Revocation) (domain.EpochVector, error) {
	var e domain.EpochVector
	if err := r.db.QueryRow(ctx, `UPDATE security_epochs SET security_epoch=security_epoch+1 WHERE singleton AND security_epoch<$1 RETURNING security_epoch`, int64(math.MaxInt64)).Scan(&e.Security); err != nil {
		return e, domain.NewError(domain.CodeConflict, "security epoch is exhausted")
	}
	if v.TenantID == nil {
		return e, nil
	}
	policy := v.Scope == domain.RevocationTenantID || v.Scope == domain.RevocationPolicyVersion
	if policy {
		if err := r.db.QueryRow(ctx, `INSERT INTO tenant_security_epochs(tenant_id,policy_epoch,revocation_epoch) VALUES($1,1,1) ON CONFLICT(tenant_id) DO UPDATE SET policy_epoch=tenant_security_epochs.policy_epoch+1,revocation_epoch=tenant_security_epochs.revocation_epoch+1 WHERE tenant_security_epochs.policy_epoch<$2 AND tenant_security_epochs.revocation_epoch<$2 RETURNING policy_epoch,revocation_epoch`, v.TenantID.String(), int64(math.MaxInt64)).Scan(&e.TenantPolicy, &e.TenantRevocation); err != nil {
			return e, domain.NewError(domain.CodeConflict, "tenant epoch is exhausted")
		}
	} else {
		if err := r.db.QueryRow(ctx, `INSERT INTO tenant_security_epochs(tenant_id,policy_epoch,revocation_epoch) VALUES($1,0,1) ON CONFLICT(tenant_id) DO UPDATE SET revocation_epoch=tenant_security_epochs.revocation_epoch+1 WHERE tenant_security_epochs.revocation_epoch<$2 RETURNING policy_epoch,revocation_epoch`, v.TenantID.String(), int64(math.MaxInt64)).Scan(&e.TenantPolicy, &e.TenantRevocation); err != nil {
			return e, domain.NewError(domain.CodeConflict, "tenant epoch is exhausted")
		}
	}
	var agent string
	if v.Scope == domain.RevocationAgentID {
		agent = v.Target
	} else if v.Scope == domain.RevocationAgentVersion {
		if err := r.db.QueryRow(ctx, `SELECT agent_id::text FROM agent_versions WHERE tenant_id=$1 AND content_digest=$2`, v.TenantID.String(), v.Target).Scan(&agent); errors.Is(err, pgx.ErrNoRows) {
			return e, domain.NewError(domain.CodeInvalidArgument, "agent-version revocation target is invalid")
		} else if err != nil {
			return e, fmt.Errorf("resolve agent-version revocation target: %w", err)
		}
	}
	if agent != "" {
		if _, err := domain.ParseID(agent); err != nil {
			return e, domain.NewError(domain.CodeInvalidArgument, "agent revocation target is invalid")
		}
		if err := r.db.QueryRow(ctx, `INSERT INTO agent_security_epochs(tenant_id,agent_id,revocation_epoch) VALUES($1,$2,1) ON CONFLICT(tenant_id,agent_id) DO UPDATE SET revocation_epoch=agent_security_epochs.revocation_epoch+1 WHERE agent_security_epochs.revocation_epoch<$3 RETURNING revocation_epoch`, v.TenantID.String(), agent, int64(math.MaxInt64)).Scan(&e.AgentRevocation); err != nil {
			return e, domain.NewError(domain.CodeConflict, "agent epoch is exhausted")
		}
	}
	return e, nil
}
func nullableEpoch(t *domain.ID, v int64) any {
	if t == nil {
		return nil
	}
	return v
}
func nullableAgentEpoch(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
