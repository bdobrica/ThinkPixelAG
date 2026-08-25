package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.RunAdmissionRepository = (*TenantRepository)(nil)

func (r *TenantRepository) AdmitRun(ctx context.Context, admission domain.RunAdmission, resolution domain.RunVersionResolution, evidence ports.RunAdmissionEvidence) error {
	if err := r.valid(); err != nil {
		return err
	}
	if admission.TenantID != r.tenantID || resolution.TenantID != r.tenantID || resolution.RunID != admission.RunID || resolution.AgentID != admission.AgentID || resolution.AgentVersionID != admission.AgentVersionID || resolution.InvocationDecisionID != admission.PolicyDecisionID {
		return errors.New("run admission does not match repository scope or resolution")
	}
	if err := admission.Validate(); err != nil {
		return err
	}
	if err := resolution.Validate(); err != nil {
		return err
	}
	resourceGrants, err := domain.RootResourceGrants(resolution.ResolvedConstraints)
	if err != nil {
		return domain.WrapError(domain.CodeInvalidArgument, "resolved resource grants are invalid", err)
	}
	if evidence.EventID.IsZero() || evidence.AuditID.IsZero() || evidence.OutboxID.IsZero() || evidence.RequestID.IsZero() || len(evidence.ReasonCodes) == 0 {
		return errors.New("run admission evidence is invalid")
	}
	constraints, err := json.Marshal(admission.Constraints)
	if err != nil || len(constraints) > 64<<10 {
		return domain.NewError(domain.CodeInvalidArgument, "run constraints are invalid or exceed bounds")
	}
	resolutionJSON, err := json.Marshal(storedResolutionEvidence{Mode: string(resolution.Mode), InvocationDecisionID: resolution.InvocationDecisionID.String(), SelectionDecisionID: optionalResolutionID(resolution.SelectionDecisionID), ResolvedConstraints: resolution.ResolvedConstraints})
	if err != nil || len(resolutionJSON) > 64<<10 {
		return domain.NewError(domain.CodeInvalidArgument, "version resolution evidence exceeds bounds")
	}
	reasons, _ := json.Marshal(evidence.ReasonCodes)
	eventPayload, _ := json.Marshal(map[string]any{"agent_id": admission.AgentID.String(), "agent_version_id": admission.AgentVersionID.String(), "envelope_id": admission.EnvelopeID.String()})
	outboxPayload, _ := json.Marshal(map[string]any{"run_id": admission.RunID.String(), "agent_id": admission.AgentID.String(), "agent_version_id": admission.AgentVersionID.String(), "state": admission.State, "state_version": admission.StateVersion, "envelope_id": admission.EnvelopeID.String()})
	auditMetadata, _ := json.Marshal(map[string]any{"agent_id": admission.AgentID.String(), "agent_version_id": admission.AgentVersionID.String(), "envelope_id": admission.EnvelopeID.String(), "policy_bundle_digest": resolution.PolicyBundleDigest, "policy_activation_version": resolution.PolicyActivationVersion})
	tenant, principal, decision, request := admission.TenantID, admission.RequestedBy, admission.PolicyDecisionID, evidence.RequestID
	audit := AuditEvent{ID: evidence.AuditID, TenantID: &tenant, PrincipalID: &principal, Action: "runs.create", ResourceType: "run", ResourceID: admission.RunID.String(), Outcome: "SUCCEEDED", ReasonCodes: reasons, PolicyDecisionID: &decision, RequestID: &request, Metadata: auditMetadata, OccurredAt: admission.CreatedAt}
	message := OutboxMessage{ID: evidence.OutboxID, TenantID: &tenant, AggregateType: "run", AggregateID: admission.RunID.String(), EventType: "run.admitted", SchemaVersion: 1, Payload: outboxPayload, Headers: json.RawMessage(`{}`), OccurredAt: admission.CreatedAt, AvailableAt: admission.CreatedAt}
	if err := validateEvidence(audit, message); err != nil {
		return err
	}
	eventHash, err := hashAuditEvent(audit)
	if err != nil {
		return err
	}
	return r.withAdmissionTransaction(ctx, func(txRepository *TenantRepository) error {
		var inserted string
		err := txRepository.db.QueryRow(ctx, `WITH locked_version AS (
 SELECT v.id FROM agent_versions v
 WHERE v.tenant_id=$1 AND v.agent_id=$3 AND v.id=$4 AND v.content_digest=$5 FOR UPDATE
), eligible_version AS (
 SELECT v.id FROM locked_version v
 JOIN LATERAL (SELECT decision FROM agent_version_approvals a WHERE a.tenant_id=$1 AND a.agent_version_id=v.id ORDER BY a.created_at DESC,a.id DESC LIMIT 1) state ON true
 WHERE state.decision='APPROVED' OR ($12='ROLLBACK' AND state.decision='DEPRECATED')
), inserted_run AS (
 INSERT INTO runs(id,tenant_id,agent_id,agent_version_id,requested_by,state,state_version,constraints,deadline_at,created_at,updated_at)
 SELECT $2,$1,$3,$4,$6,'ADMITTED',1,$7::jsonb,$8,$9,$9 FROM eligible_version RETURNING id
), inserted_resolution AS (
 INSERT INTO run_version_resolutions(run_id,tenant_id,agent_id,agent_version_id,agent_content_digest,policy_bundle_digest,policy_activation_version,approval_id,resolution,resolved_at)
 SELECT $2,$1,$3,$4,$5,$10,$11,$13,$14::jsonb,$15 FROM inserted_run
 JOIN agent_version_approvals approval ON approval.tenant_id=$1 AND approval.id=$13 AND approval.agent_id=$3 AND approval.agent_version_id=$4 AND approval.decision='APPROVED'
 RETURNING run_id
), inserted_envelope AS (
 INSERT INTO resource_envelopes(id,tenant_id,run_id,version,issued_by,policy_decision_id,issued_at)
 SELECT $16,$1,$2,1,$6,$17,$9 FROM inserted_resolution RETURNING run_id
), inserted_event AS (
 INSERT INTO run_events(id,tenant_id,run_id,sequence,event_type,actor_type,actor_id,state,state_version,payload,occurred_at)
 SELECT $18,$1,$2,1,'run.admitted','SYSTEM',NULL,'ADMITTED',1,$19::jsonb,$9 FROM inserted_envelope RETURNING run_id
), inserted_audit AS (
 INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,reason_codes,policy_decision_id,request_id,metadata,event_hash,occurred_at)
 SELECT $20,$1,$6,'runs.create','run',$2::text,'SUCCEEDED',$21::jsonb,$17,$22,$23::jsonb,$24,$9 FROM inserted_event RETURNING resource_id
), inserted_outbox AS (
 INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at)
 SELECT $25,$1,'run',$2::text,'run.admitted',1,$26::jsonb,'{}'::jsonb,$9,$9 FROM inserted_audit RETURNING aggregate_id
) SELECT aggregate_id FROM inserted_outbox`, r.tenantID.String(), admission.RunID.String(), admission.AgentID.String(), admission.AgentVersionID.String(), resolution.AgentContentDigest, admission.RequestedBy.String(), constraints, admission.DeadlineAt, admission.CreatedAt, resolution.PolicyBundleDigest, resolution.PolicyActivationVersion, string(resolution.Mode), resolution.ApprovalID.String(), resolutionJSON, resolution.ResolvedAt, admission.EnvelopeID.String(), admission.PolicyDecisionID.String(), evidence.EventID.String(), eventPayload, evidence.AuditID.String(), reasons, evidence.RequestID.String(), auditMetadata, eventHash, evidence.OutboxID.String(), outboxPayload).Scan(&inserted)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeConflict, "agent version is no longer eligible for admission")
		}
		if err != nil {
			switch ClassifyError(err) {
			case ErrorUniqueViolation:
				return domain.WrapError(domain.CodeConflict, "run admission identifier already exists", err)
			case ErrorForeignKeyViolation, ErrorCheckViolation:
				return domain.WrapError(domain.CodeInvalidArgument, "run admission references are invalid", err)
			default:
				return fmt.Errorf("admit run: %w", err)
			}
		}
		for _, grant := range resourceGrants {
			commandTag, grantErr := txRepository.db.Exec(ctx, `WITH dimension AS (
 SELECT id,unit,scale,minimum_value,maximum_value FROM resource_dimensions
 WHERE tenant_id=$1 AND name=$3
), inserted_grant AS (
 INSERT INTO resource_envelope_grants(tenant_id,envelope_id,dimension_id,granted_value,unit,scale)
 SELECT $1,$2,id,$4,unit,scale FROM dimension
 WHERE $4 BETWEEN minimum_value AND maximum_value
 RETURNING dimension_id,unit,scale
)
INSERT INTO resource_balances(tenant_id,envelope_id,dimension_id,available_value,direct_consumed_value,allocated_open_value,state_version,updated_at)
SELECT $1,$2,dimension_id,$4,0,0,1,$5 FROM inserted_grant`, admission.TenantID.String(), admission.EnvelopeID.String(), grant.DimensionName, grant.Coefficient, admission.CreatedAt)
			if grantErr != nil {
				return fmt.Errorf("issue root resource grant %q: %w", grant.DimensionName, grantErr)
			}
			if commandTag.RowsAffected() != 1 {
				return domain.NewError(domain.CodeUnavailable, "policy resource dimension is unavailable or outside configured bounds").WithRetryable()
			}
		}
		return nil
	})
}

func (r *TenantRepository) withAdmissionTransaction(ctx context.Context, fn func(*TenantRepository) error) error {
	if beginner, ok := r.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	}); ok {
		tx, err := beginner.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin run admission: %w", err)
		}
		err = fn(&TenantRepository{db: tx, tenantID: r.tenantID})
		if err != nil {
			if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit run admission: %w", err)
		}
		return nil
	}
	return fn(r)
}

func optionalResolutionID(id domain.ID) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}
