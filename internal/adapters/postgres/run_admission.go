package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.RunAdmissionRepository = (*TenantRepository)(nil)
var _ ports.ChildRunAdmissionRepository = (*TenantRepository)(nil)

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
		// Admissions can share an immutable version, but must exclude approval
		// changes until commit. Read eligibility in the next statement so an
		// approval that committed while this lock waited is visible under READ COMMITTED.
		var versionID string
		err := txRepository.db.QueryRow(ctx, `SELECT id::text FROM agent_versions
WHERE tenant_id=$1 AND agent_id=$2 AND id=$3 AND content_digest=$4 FOR SHARE`, r.tenantID.String(), admission.AgentID.String(), admission.AgentVersionID.String(), resolution.AgentContentDigest).Scan(&versionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeConflict, "agent version is no longer eligible for admission")
		}
		if err != nil {
			return fmt.Errorf("lock agent version for admission: %w", err)
		}
		var inserted string
		err = txRepository.db.QueryRow(ctx, `WITH selected_version AS (
 SELECT v.id FROM agent_versions v
 WHERE v.tenant_id=$1 AND v.agent_id=$3 AND v.id=$4 AND v.content_digest=$5
), eligible_version AS (
 SELECT v.id FROM selected_version v
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
		if len(resourceGrants) == 0 {
			return nil
		}
		names := make([]string, len(resourceGrants))
		values := make([]int64, len(resourceGrants))
		for i, grant := range resourceGrants {
			names[i], values[i] = grant.DimensionName, grant.Coefficient
		}
		commandTag, grantErr := txRepository.db.Exec(ctx, `WITH requested AS (
 SELECT * FROM unnest($3::text[],$4::bigint[]) AS grants(name,value)
), inserted_grant AS (
 INSERT INTO resource_envelope_grants(tenant_id,envelope_id,dimension_id,granted_value,unit,scale)
 SELECT $1,$2,d.id,g.value,d.unit,d.scale FROM requested g
 JOIN resource_dimensions d ON d.tenant_id=$1 AND d.name=g.name
 WHERE g.value BETWEEN d.minimum_value AND d.maximum_value
 RETURNING dimension_id,granted_value
)
INSERT INTO resource_balances(tenant_id,envelope_id,dimension_id,available_value,direct_consumed_value,allocated_open_value,state_version,updated_at)
SELECT $1,$2,dimension_id,granted_value,0,0,1,$5 FROM inserted_grant`, admission.TenantID.String(), admission.EnvelopeID.String(), names, values, admission.CreatedAt)
		if grantErr != nil {
			return fmt.Errorf("issue root resource grants: %w", grantErr)
		}
		if commandTag.RowsAffected() != int64(len(resourceGrants)) {
			return domain.NewError(domain.CodeUnavailable, "policy resource dimension is unavailable or outside configured bounds").WithRetryable()
		}
		return nil
	})
}

// AdmitChildRun composes the already-authorized run aggregate with its parent
// link, topology checks, and consumable reservation under one transaction. A
// structural or consumable conflict therefore leaves no child or evidence.
func (r *TenantRepository) AdmitChildRun(ctx context.Context, admission domain.RunAdmission, resolution domain.RunVersionResolution, evidence ports.RunAdmissionEvidence, reservation domain.ResourceReservation) (domain.ResourceReservation, error) {
	if reservation.TenantID != admission.TenantID || reservation.ChildRunID != admission.RunID || reservation.ChildEnvelopeID != admission.EnvelopeID || reservation.ParentEnvelopeID.IsZero() {
		return domain.ResourceReservation{}, domain.NewError(domain.CodeInvalidArgument, "child admission reservation does not match the run")
	}
	var admitted domain.ResourceReservation
	err := r.withAdmissionTransaction(ctx, func(repository *TenantRepository) error {
		if err := repository.AdmitRun(ctx, admission, resolution, evidence); err != nil {
			return err
		}
		// Lock before the parent foreign key takes KEY SHARE. Concurrent children
		// must not each hold KEY SHARE and then try to upgrade it for topology checks.
		if err := repository.lockParentResourceEnvelope(ctx, reservation.ParentEnvelopeID); err != nil {
			return err
		}
		tag, err := repository.db.Exec(ctx, `UPDATE resource_envelopes SET parent_envelope_id=$3 WHERE tenant_id=$1 AND id=$2 AND parent_envelope_id IS NULL`, r.tenantID.String(), admission.EnvelopeID.String(), reservation.ParentEnvelopeID.String())
		if err != nil {
			return fmt.Errorf("link child resource envelope: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return domain.NewError(domain.CodeConflict, "child resource envelope could not be linked")
		}
		admitted, err = repository.ReserveChildResources(ctx, reservation)
		return err
	})
	if err != nil {
		return domain.ResourceReservation{}, err
	}
	return admitted, nil
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

var _ ports.IdempotentRunAdmissionRepository = (*TenantRepository)(nil)

func (r *TenantRepository) AdmitRunIdempotently(ctx context.Context, admission domain.RunAdmission, resolution domain.RunVersionResolution, evidence ports.RunAdmissionEvidence, acquisition ports.IdempotencyAcquisition, response ports.IdempotencyResponse, now time.Time) error {
	if err := r.valid(); err != nil {
		return err
	}
	if acquisition.Outcome != ports.IdempotencyAcquired || acquisition.RecordID.IsZero() || acquisition.OwnerToken.IsZero() || admission.TenantID != r.tenantID {
		return domain.NewError(domain.CodeConflict, "admission idempotency ownership is invalid")
	}
	pool, ok := r.db.(*pgxpool.Pool)
	if !ok {
		return domain.NewError(domain.CodeUnavailable, "idempotent admission requires a transaction-owning repository")
	}
	transactor, err := NewTransactor(pool)
	if err != nil {
		return err
	}
	return transactor.WithinTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, db DBTX) error {
		repository := &TenantRepository{db: db, tenantID: r.tenantID}
		// Hold ownership through the entire mutation. A lease-expiry contender must
		// wait for this transaction and then replay, or acquire only after rollback.
		var id string
		err := repository.db.QueryRow(ctx, `SELECT id::text FROM idempotency_records
WHERE tenant_id=$1 AND id=$2 AND owner_token=$3 AND principal_id=$4
AND state='IN_PROGRESS' AND route='/v1/agents/{agent_id}/runs' FOR UPDATE`, r.tenantID.String(), acquisition.RecordID.String(), acquisition.OwnerToken.String(), admission.RequestedBy.String()).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeConflict, "admission idempotency ownership lost")
		}
		if err != nil {
			return fmt.Errorf("lock admission idempotency: %w", err)
		}
		if err := repository.AdmitRun(ctx, admission, resolution, evidence); err != nil {
			return err
		}
		if err := repository.CompleteIdempotency(ctx, acquisition, response, now); err != nil {
			return domain.WrapError(domain.CodeUnavailable, "could not establish idempotent response", err).WithRetryable()
		}
		return nil
	})
}
