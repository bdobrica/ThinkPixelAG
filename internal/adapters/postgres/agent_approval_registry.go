package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.AgentApprovalRegistry = (*TenantRepository)(nil)

func (r *TenantRepository) RecordAgentVersionDecision(ctx context.Context, approval domain.AgentVersionApproval, digest string, auditID, outboxID domain.ID, requestID *domain.ID) (domain.AgentVersionApproval, error) {
	if err := r.valid(); err != nil {
		return domain.AgentVersionApproval{}, err
	}
	if approval.TenantID != r.tenantID || auditID.IsZero() || outboxID.IsZero() || len(digest) != 71 {
		return domain.AgentVersionApproval{}, errors.New("agent version decision does not match repository scope")
	}
	reasons, _ := json.Marshal([]string{approval.ReasonCode})
	payload, _ := json.Marshal(map[string]string{"approval_id": approval.ID.String(), "agent_id": approval.AgentID.String(), "version_digest": digest, "decision": string(approval.Decision), "reason_code": approval.ReasonCode})
	metadata, _ := json.Marshal(map[string]string{"approval_reference": approval.ApprovalReference, "version_digest": digest})
	hashInput, _ := json.Marshal([]any{approval.ID.String(), approval.TenantID.String(), approval.ActorPrincipalID.String(), approval.Decision, approval.ReasonCode, approval.CreatedAt})
	hash := sha256.Sum256(hashInput)
	eventHash := "sha256:" + hex.EncodeToString(hash[:])
	var versionID string
	err := r.db.QueryRow(ctx, `WITH locked_version AS (
  SELECT id FROM agent_versions WHERE tenant_id=$1 AND agent_id=$2 AND content_digest=$3 FOR UPDATE
), current_state AS (
  SELECT locked_version.id, COALESCE((SELECT decision FROM agent_version_approvals
    WHERE tenant_id=$1 AND agent_version_id=locked_version.id ORDER BY created_at DESC,id DESC LIMIT 1),'REGISTERED') AS state
  FROM locked_version
), inserted_approval AS (
  INSERT INTO agent_version_approvals(id,tenant_id,agent_id,agent_version_id,decision,actor_principal_id,policy_decision_id,reason_code,approval_reference,created_at)
  SELECT $4,$1,$2,id,$5,$6,$7,$8,NULLIF($9,''),$10 FROM current_state
  WHERE (state='REGISTERED' AND $5 IN ('APPROVED','REJECTED'))
     OR (state='APPROVED' AND $5 IN ('DEPRECATED','REVOKED'))
     OR (state='DEPRECATED' AND $5='REVOKED')
  RETURNING agent_version_id
), inserted_audit AS (
  INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,reason_codes,policy_decision_id,request_id,metadata,event_hash,occurred_at)
  SELECT $11,$1,$6,'agent_versions.' || lower($5),'agent_version',$3,'SUCCEEDED',$12::jsonb,$7,$13,$14::jsonb,$15,$10 FROM inserted_approval
), inserted_outbox AS (
  INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at)
  SELECT $16,$1,'agent_version',$3,'agent.version.' || lower($5),1,$17::jsonb,'{}'::jsonb,$10,$10 FROM inserted_approval
)
SELECT agent_version_id::text FROM inserted_approval`, r.tenantID.String(), approval.AgentID.String(), digest, approval.ID.String(), string(approval.Decision), approval.ActorPrincipalID.String(), approval.PolicyDecisionID.String(), approval.ReasonCode, approval.ApprovalReference, approval.CreatedAt, auditID.String(), reasons, optionalDBID(requestID), metadata, eventHash, outboxID.String(), payload).Scan(&versionID)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if lookupErr := r.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_versions WHERE tenant_id=$1 AND agent_id=$2 AND content_digest=$3)`, r.tenantID.String(), approval.AgentID.String(), digest).Scan(&exists); lookupErr != nil {
			return domain.AgentVersionApproval{}, fmt.Errorf("inspect agent version decision conflict: %w", lookupErr)
		}
		if !exists {
			return domain.AgentVersionApproval{}, domain.NewError(domain.CodeNotFound, "agent version not found")
		}
		return domain.AgentVersionApproval{}, domain.NewError(domain.CodeConflict, "agent version decision is not allowed from its current state")
	}
	if err != nil {
		switch ClassifyError(err) {
		case ErrorUniqueViolation:
			return domain.AgentVersionApproval{}, domain.WrapError(domain.CodeConflict, "agent version decision identifier already exists", err)
		case ErrorForeignKeyViolation, ErrorCheckViolation:
			return domain.AgentVersionApproval{}, domain.WrapError(domain.CodeInvalidArgument, "agent version decision references are invalid", err)
		default:
			return domain.AgentVersionApproval{}, fmt.Errorf("record agent version decision: %w", err)
		}
	}
	parsed, err := domain.ParseID(versionID)
	if err != nil {
		return domain.AgentVersionApproval{}, fmt.Errorf("decode decided agent version ID: %w", err)
	}
	approval.AgentVersionID = parsed
	if err := approval.Validate(); err != nil {
		return domain.AgentVersionApproval{}, fmt.Errorf("validate stored agent version decision: %w", err)
	}
	return approval, nil
}

func (r *TenantRepository) AgentVersionEligibility(ctx context.Context, agentID domain.ID, digest string) (domain.AgentVersionState, error) {
	if err := r.valid(); err != nil {
		return "", err
	}
	var state string
	err := r.db.QueryRow(ctx, `SELECT COALESCE((SELECT decision FROM agent_version_approvals a
 WHERE a.tenant_id=v.tenant_id AND a.agent_version_id=v.id ORDER BY created_at DESC,a.id DESC LIMIT 1),'REGISTERED')
FROM agent_versions v WHERE v.tenant_id=$1 AND v.agent_id=$2 AND v.content_digest=$3`, r.tenantID.String(), agentID.String(), digest).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.NewError(domain.CodeNotFound, "agent version not found")
	}
	if err != nil {
		return "", fmt.Errorf("read agent version eligibility: %w", err)
	}
	return domain.AgentVersionState(state), nil
}
