package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.ResourceExtensionRepository = (*TenantRepository)(nil)

func (r *TenantRepository) ExtendResources(ctx context.Context, candidate domain.ResourceExtension, evidence ports.ResourceExtensionEvidence) (result domain.ResourceExtensionResult, err error) {
	if err := r.valid(); err != nil {
		return result, err
	}
	extension, err := domain.ValidateResourceExtension(candidate)
	if err != nil {
		return result, err
	}
	if extension.TenantID != r.tenantID {
		return result, errors.New("resource extension does not match repository scope")
	}
	if evidence.AuditID.IsZero() || evidence.OutboxID.IsZero() || evidence.EventID.IsZero() || evidence.RequestID.IsZero() || len(evidence.ReasonCodes) == 0 {
		return result, errors.New("resource extension evidence is invalid")
	}
	digest := resourceExtensionDigest(extension)
	err = r.withAdmissionTransaction(ctx, func(txr *TenantRepository) error {
		idempotencyLock := fmt.Sprintf("%s:%s:%d:%s", r.tenantID, extension.ActorPrincipalID, len(extension.IdempotencyKey), extension.IdempotencyKey)
		if _, err := txr.db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, idempotencyLock); err != nil {
			return fmt.Errorf("lock resource extension idempotency key: %w", err)
		}
		var existingID, existingDigest string
		var existingVersion int64
		var existingDeadline *time.Time
		var existingResumed bool
		replayErr := txr.db.QueryRow(ctx, `SELECT id::text,content_digest,new_envelope_version,new_deadline_at,resumed_run FROM resource_extensions WHERE tenant_id=$1 AND actor_principal_id=$2 AND idempotency_key=$3`, r.tenantID.String(), extension.ActorPrincipalID.String(), extension.IdempotencyKey).Scan(&existingID, &existingDigest, &existingVersion, &existingDeadline, &existingResumed)
		if replayErr == nil {
			if existingDigest != digest {
				return domain.NewError(domain.CodeConflict, "resource extension idempotency key was reused with different content")
			}
			id, _ := domain.ParseID(existingID)
			result = domain.ResourceExtensionResult{ID: id, EnvelopeVersion: existingVersion, DeadlineAt: existingDeadline, Resumed: existingResumed}
			return nil
		} else if !errors.Is(replayErr, pgx.ErrNoRows) {
			return fmt.Errorf("query resource extension replay: %w", replayErr)
		}

		var envelopeID, state, requestedBy string
		var envelopeVersion, stateVersion, fencingToken int64
		var deadline *time.Time
		var updatedAt time.Time
		if err := txr.db.QueryRow(ctx, `SELECT e.id::text,e.version,ru.state,ru.state_version,ru.deadline_at,ru.updated_at,ru.fencing_token,ru.requested_by::text FROM resource_envelopes e JOIN runs ru ON ru.tenant_id=e.tenant_id AND ru.id=e.run_id JOIN principals p ON p.tenant_id=ru.tenant_id AND p.id=$3 AND p.disabled_at IS NULL WHERE e.tenant_id=$1 AND e.run_id=$2 FOR UPDATE OF e,ru`, r.tenantID.String(), extension.RunID.String(), extension.ActorPrincipalID.String()).Scan(&envelopeID, &envelopeVersion, &state, &stateVersion, &deadline, &updatedAt, &fencingToken, &requestedBy); errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeNotFound, "run not found")
		} else if err != nil {
			return fmt.Errorf("lock resource extension target: %w", err)
		}
		if requestedBy == extension.ActorPrincipalID.String() {
			return domain.NewError(domain.CodeForbidden, "a run requester cannot extend its own resources")
		}
		if domain.RunState(state).Terminal() {
			return domain.NewError(domain.CodeConflict, "terminal run resources cannot be extended")
		}
		if envelopeVersion == math.MaxInt64 {
			return domain.NewError(domain.CodeConflict, "resource envelope version is exhausted")
		}
		newDeadline := deadline
		if extension.DeadlineExtensionSeconds > 0 {
			if deadline == nil {
				return domain.NewError(domain.CodeConflict, "run has no deadline to extend")
			}
			if extension.DeadlineExtensionSeconds > math.MaxInt64/int64(time.Second) {
				return domain.NewError(domain.CodeInvalidArgument, "deadline extension overflows")
			}
			candidateDeadline := deadline.Add(time.Duration(extension.DeadlineExtensionSeconds) * time.Second)
			if !candidateDeadline.After(*deadline) {
				return domain.NewError(domain.CodeConflict, "deadline extension overflows")
			}
			newDeadline = &candidateDeadline
		}
		newVersion := envelopeVersion + 1
		resumed := domain.RunState(state) == domain.RunPausedForBudget
		if _, err := txr.db.Exec(ctx, `INSERT INTO resource_extensions(id,tenant_id,run_id,envelope_id,actor_principal_id,policy_decision_id,idempotency_key,reason_code,approval_reference,content_digest,prior_deadline_at,new_deadline_at,prior_envelope_version,new_envelope_version,resumed_run,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, extension.ID.String(), r.tenantID.String(), extension.RunID.String(), envelopeID, extension.ActorPrincipalID.String(), extension.PolicyDecisionID.String(), extension.IdempotencyKey, extension.ReasonCode, extension.ApprovalReference, digest, deadline, newDeadline, envelopeVersion, newVersion, resumed, extension.CreatedAt); err != nil {
			return fmt.Errorf("append resource extension: %w", err)
		}
		for _, addition := range extension.Additions {
			var dimensionID, class, unit string
			var scale int16
			var original, previousExtensions int64
			err := txr.db.QueryRow(ctx, `SELECT d.id::text,d.class,g.unit,g.scale,g.granted_value,COALESCE((SELECT sum(i.added_value) FROM resource_extension_items i JOIN resource_extensions x ON x.tenant_id=i.tenant_id AND x.id=i.extension_id WHERE i.tenant_id=g.tenant_id AND x.envelope_id=g.envelope_id AND i.dimension_id=g.dimension_id),0) FROM resource_envelope_grants g JOIN resource_dimensions d ON d.tenant_id=g.tenant_id AND d.id=g.dimension_id WHERE g.tenant_id=$1 AND g.envelope_id=$2 AND d.name=$3 FOR UPDATE OF g`, r.tenantID.String(), envelopeID, addition.Name).Scan(&dimensionID, &class, &unit, &scale, &original, &previousExtensions)
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError(domain.CodeInvalidArgument, "extension resource is not in the envelope")
			} else if err != nil {
				return fmt.Errorf("lock extension dimension: %w", err)
			}
			if domain.ResourceClass(class) != domain.ResourceConsumable || unit != addition.Quantity.Unit() || uint8(scale) != addition.Quantity.Amount().Scale() {
				return domain.NewError(domain.CodeInvalidArgument, "only matching consumable grants can be extended")
			}
			added := addition.Quantity.Amount().Coefficient()
			if original > math.MaxInt64-previousExtensions || original+previousExtensions > math.MaxInt64-added {
				return domain.NewError(domain.CodeConflict, "resource grant extension overflows")
			}
			prior := original + previousExtensions
			next := prior + added
			if _, err := txr.db.Exec(ctx, `INSERT INTO resource_extension_items(tenant_id,extension_id,dimension_id,added_value,prior_granted_value,new_granted_value,unit,scale) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, r.tenantID.String(), extension.ID.String(), dimensionID, added, prior, next, unit, scale); err != nil {
				return fmt.Errorf("append resource extension item: %w", err)
			}
			tag, err := txr.db.Exec(ctx, `UPDATE resource_balances SET available_value=available_value+$4,state_version=state_version+1,updated_at=$5 WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3 AND available_value <= 9223372036854775807-$4 AND state_version < 9223372036854775807`, r.tenantID.String(), envelopeID, dimensionID, added, extension.CreatedAt)
			if err != nil {
				return fmt.Errorf("credit resource extension: %w", err)
			}
			if tag.RowsAffected() != 1 {
				return domain.NewError(domain.CodeConflict, "resource balance extension overflows")
			}
		}
		newState, newStateVersion := state, stateVersion
		if resumed {
			lifecycle := domain.RunLifecycle{State: domain.RunPausedForBudget, Version: stateVersion, UpdatedAt: updatedAt.UTC()}
			next, _, transitionErr := lifecycle.Transition(domain.RunTransition{To: domain.RunRunning, Actor: domain.RunActorGovernor, ExpectedVersion: stateVersion, At: extension.CreatedAt})
			if transitionErr != nil {
				return domain.NewError(domain.CodeConflict, "paused run cannot resume after extension")
			}
			newState, newStateVersion = string(next.State), next.Version
		}
		if _, err := txr.db.Exec(ctx, `UPDATE resource_envelopes SET version=$3 WHERE tenant_id=$1 AND id=$2`, r.tenantID.String(), envelopeID, newVersion); err != nil {
			return fmt.Errorf("version extended envelope: %w", err)
		}
		if _, err := txr.db.Exec(ctx, `UPDATE runs SET deadline_at=$3,state=$4,state_version=$5,updated_at=$6 WHERE tenant_id=$1 AND id=$2`, r.tenantID.String(), extension.RunID.String(), newDeadline, newState, newStateVersion, extension.CreatedAt); err != nil {
			return fmt.Errorf("update extended run: %w", err)
		}
		var sequence int64
		if err := txr.db.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM run_events WHERE tenant_id=$1 AND run_id=$2`, r.tenantID.String(), extension.RunID.String()).Scan(&sequence); err != nil {
			return fmt.Errorf("sequence extension event: %w", err)
		}
		payload, _ := json.Marshal(map[string]any{"extension_id": extension.ID.String(), "envelope_version": newVersion, "reason_code": extension.ReasonCode, "resumed": resumed})
		if _, err := txr.db.Exec(ctx, `INSERT INTO run_events(id,tenant_id,run_id,sequence,event_type,actor_type,actor_id,state,state_version,payload,occurred_at) VALUES($1,$2,$3,$4,'run.resources_extended','OPERATOR',$5,$6,$7,$8,$9)`, evidence.EventID.String(), r.tenantID.String(), extension.RunID.String(), sequence, extension.ActorPrincipalID.String(), newState, newStateVersion, payload, extension.CreatedAt); err != nil {
			return fmt.Errorf("append extension event: %w", err)
		}
		reasons, _ := json.Marshal(evidence.ReasonCodes)
		metadata, _ := json.Marshal(map[string]any{"approval_reference": extension.ApprovalReference, "envelope_version": newVersion, "extension_id": extension.ID.String(), "reason_code": extension.ReasonCode, "resumed": resumed})
		tenant, actor, decision, request := extension.TenantID, extension.ActorPrincipalID, extension.PolicyDecisionID, evidence.RequestID
		audit := AuditEvent{ID: evidence.AuditID, TenantID: &tenant, PrincipalID: &actor, Action: "resources.extend", ResourceType: "envelope", ResourceID: envelopeID, Outcome: "SUCCEEDED", ReasonCodes: reasons, PolicyDecisionID: &decision, RequestID: &request, Metadata: metadata, OccurredAt: extension.CreatedAt}
		message := OutboxMessage{ID: evidence.OutboxID, TenantID: &tenant, AggregateType: "run", AggregateID: extension.RunID.String(), EventType: "run.resources_extended", SchemaVersion: 1, Payload: payload, Headers: json.RawMessage(`{}`), OccurredAt: extension.CreatedAt, AvailableAt: extension.CreatedAt}
		if err := validateEvidence(audit, message); err != nil {
			return err
		}
		eventHash, err := hashAuditEvent(audit)
		if err != nil {
			return err
		}
		if _, err := txr.db.Exec(ctx, `WITH a AS (INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,reason_codes,policy_decision_id,request_id,metadata,event_hash,occurred_at) VALUES($1,$2,$3,'resources.extend','envelope',$4,'SUCCEEDED',$5::jsonb,$6,$7,$8::jsonb,$9,$10) RETURNING resource_id) INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) SELECT $11,$2,'run',$12,'run.resources_extended',1,$13::jsonb,'{}'::jsonb,$10,$10 FROM a`, evidence.AuditID.String(), r.tenantID.String(), extension.ActorPrincipalID.String(), envelopeID, reasons, extension.PolicyDecisionID.String(), evidence.RequestID.String(), metadata, eventHash, extension.CreatedAt, evidence.OutboxID.String(), extension.RunID.String(), payload); err != nil {
			return fmt.Errorf("append extension evidence: %w", err)
		}
		result = domain.ResourceExtensionResult{ID: extension.ID, EnvelopeVersion: newVersion, DeadlineAt: newDeadline, Resumed: resumed}
		return nil
	})
	return result, err
}

func resourceExtensionDigest(extension domain.ResourceExtension) string {
	type digestItem struct {
		Name, Unit  string
		Coefficient int64
		Scale       uint8
	}
	items := make([]digestItem, 0, len(extension.Additions))
	for _, addition := range extension.Additions {
		items = append(items, digestItem{addition.Name, addition.Quantity.Unit(), addition.Quantity.Amount().Coefficient(), addition.Quantity.Amount().Scale()})
	}
	payload, _ := json.Marshal(struct {
		Run, Reason, Approval string
		Additions             []digestItem
		Deadline              int64
	}{extension.RunID.String(), extension.ReasonCode, extension.ApprovalReference, items, extension.DeadlineExtensionSeconds})
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
