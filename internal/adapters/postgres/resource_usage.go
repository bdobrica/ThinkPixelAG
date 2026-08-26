package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.TrustedUsageRepository = (*TenantRepository)(nil)

func (r *TenantRepository) RecordTrustedUsage(ctx context.Context, candidate domain.TrustedUsage, hint domain.ThroughputHint, disposition domain.ExhaustionDisposition) (receipt domain.UsageReceipt, err error) {
	if err = r.valid(); err != nil {
		return receipt, err
	}
	if candidate.TenantID != r.tenantID {
		return receipt, errors.New("trusted usage does not match repository scope")
	}
	if !disposition.Valid() {
		return receipt, domain.NewError(domain.CodeInvalidArgument, "exhaustion disposition is invalid")
	}
	usage, err := domain.ValidateTrustedUsage(candidate)
	if err != nil {
		return receipt, domain.WrapError(domain.CodeInvalidArgument, "trusted usage is invalid", err)
	}
	err = r.withResourceTransaction(ctx, func(repository *TenantRepository) error {
		// Serialize one producer/source key even before its first row exists so
		// concurrent deliveries cannot apply the same usage twice.
		lockKey := fmt.Sprintf("%s:%s:%d:%s", r.tenantID, usage.ProducerID, len(usage.SourceEventID), usage.SourceEventID)
		if _, err := repository.db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey); err != nil {
			return fmt.Errorf("lock trusted usage source event: %w", err)
		}
		var existingID, existingDigest string
		var recordedAt = usage.RecordedAt
		err := repository.db.QueryRow(ctx, `SELECT id::text,content_digest,recorded_at FROM trusted_usage_entries WHERE tenant_id=$1 AND producer_id=$2 AND source_event_id=$3 FOR UPDATE`, r.tenantID.String(), usage.ProducerID.String(), usage.SourceEventID).Scan(&existingID, &existingDigest, &recordedAt)
		if err == nil {
			id, parseErr := domain.ParseID(existingID)
			if parseErr != nil {
				return fmt.Errorf("decode trusted usage identifier: %w", parseErr)
			}
			if existingDigest != usage.ContentDigest {
				return domain.NewError(domain.CodeConflict, "source event identifier was reused with different content")
			}
			receipt = domain.UsageReceipt{UsageID: id, Duplicate: true, AcceptedAt: recordedAt.UTC()}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("query trusted usage replay: %w", err)
		}
		var envelopeID, dimensionID, class, unit, runState string
		var scale int16
		var runVersion, fencingToken int64
		var runUpdatedAt time.Time
		err = repository.db.QueryRow(ctx, `SELECT e.id::text,d.id::text,d.class,g.unit,g.scale,ru.state,ru.state_version,ru.updated_at,ru.fencing_token FROM resource_envelopes e JOIN runs ru ON ru.tenant_id=e.tenant_id AND ru.id=e.run_id JOIN resource_envelope_grants g ON g.tenant_id=e.tenant_id AND g.envelope_id=e.id JOIN resource_dimensions d ON d.tenant_id=g.tenant_id AND d.id=g.dimension_id JOIN principals p ON p.tenant_id=e.tenant_id AND p.id=$3 WHERE e.tenant_id=$1 AND e.run_id=$2 AND d.name=$4 AND p.disabled_at IS NULL FOR UPDATE OF ru,e`, r.tenantID.String(), usage.RunID.String(), usage.ProducerID.String(), usage.ResourceName).Scan(&envelopeID, &dimensionID, &class, &unit, &scale, &runState, &runVersion, &runUpdatedAt, &fencingToken)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeNotFound, "metering target not found")
		}
		if err != nil {
			return fmt.Errorf("resolve trusted usage target: %w", err)
		}
		if class != string(domain.ResourceConsumable) || unit != usage.Quantity.Unit() || scale != int16(usage.Quantity.Amount().Scale()) {
			return domain.NewError(domain.CodeInvalidArgument, "trusted usage resource or unit is invalid")
		}
		if domain.RunState(runState) != domain.RunRunning {
			return domain.NewError(domain.CodeConflict, "trusted usage requires a running run")
		}
		var closed bool
		if err := repository.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM resource_reservations WHERE tenant_id=$1 AND child_envelope_id=$2 AND state<>'OPEN')`, r.tenantID.String(), envelopeID).Scan(&closed); err != nil {
			return fmt.Errorf("check usage settlement: %w", err)
		}
		if closed {
			return domain.NewError(domain.CodeConflict, "late usage is not accepted after settlement")
		}
		rateDimension, nameErr := domain.ThroughputDimensionName(usage.ResourceName)
		if nameErr == nil {
			var rateDimensionID, rateUnit string
			var rateLimit int64
			rateErr := repository.db.QueryRow(ctx, `SELECT d.id::text,d.unit,g.granted_value FROM resource_envelope_grants g JOIN resource_dimensions d ON d.tenant_id=g.tenant_id AND d.id=g.dimension_id WHERE g.tenant_id=$1 AND g.envelope_id=$2 AND d.name=$3 AND d.class='STRUCTURAL' AND d.aggregation='MAX' AND d.scale=0`, r.tenantID.String(), envelopeID, rateDimension).Scan(&rateDimensionID, &rateUnit, &rateLimit)
			if rateErr != nil && !errors.Is(rateErr, pgx.ErrNoRows) {
				return fmt.Errorf("resolve structural throughput grant: %w", rateErr)
			}
			if rateErr == nil {
				expectedRateUnit, unitErr := domain.ThroughputUnitName(usage.Quantity.Unit())
				if unitErr != nil || rateUnit != expectedRateUnit {
					return domain.NewError(domain.CodeInvalidArgument, "structural throughput unit is invalid")
				}
				retryAt := usage.RecordedAt.Truncate(domain.ThroughputWindow).Add(domain.ThroughputWindow)
				if hint.Blocked && hint.DimensionName == rateDimension {
					return &domain.ThroughputLimitExceeded{DimensionName: rateDimension, RetryAt: retryAt}
				}
				windowStart := usage.RecordedAt.Truncate(domain.ThroughputWindow)
				var used int64
				rateErr = repository.db.QueryRow(ctx, `INSERT INTO resource_rate_windows(tenant_id,envelope_id,dimension_id,window_start,used_value,updated_at) SELECT $1,$2,$3,$4,$5::bigint,$6 WHERE $5::bigint <= $7::bigint ON CONFLICT(tenant_id,envelope_id,dimension_id,window_start) DO UPDATE SET used_value=resource_rate_windows.used_value+EXCLUDED.used_value,updated_at=EXCLUDED.updated_at WHERE resource_rate_windows.used_value <= $7::bigint-EXCLUDED.used_value RETURNING used_value`, r.tenantID.String(), envelopeID, rateDimensionID, windowStart, usage.Quantity.Amount().Coefficient(), usage.RecordedAt, rateLimit).Scan(&used)
				if errors.Is(rateErr, pgx.ErrNoRows) {
					return &domain.ThroughputLimitExceeded{DimensionName: rateDimension, RetryAt: retryAt}
				}
				if rateErr != nil {
					return fmt.Errorf("apply structural throughput limit: %w", rateErr)
				}
			}
		}
		var availableAfter int64
		err = repository.db.QueryRow(ctx, `UPDATE resource_balances SET available_value=available_value-$4,direct_consumed_value=direct_consumed_value+$4,state_version=state_version+1,updated_at=$5 WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3 AND available_value >= $4 AND direct_consumed_value <= 9223372036854775807-$4 AND state_version < 9223372036854775807 RETURNING available_value`, r.tenantID.String(), envelopeID, dimensionID, usage.Quantity.Amount().Coefficient(), usage.RecordedAt).Scan(&availableAfter)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.NewError(domain.CodeConflict, "trusted usage exceeds available resource")
			}
			return fmt.Errorf("apply trusted usage: %w", err)
		}
		_, err = repository.db.Exec(ctx, `INSERT INTO trusted_usage_entries(id,tenant_id,run_id,envelope_id,dimension_id,producer_id,source_event_id,quantity_value,unit,scale,content_digest,observed_at,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, usage.ID.String(), r.tenantID.String(), usage.RunID.String(), envelopeID, dimensionID, usage.ProducerID.String(), usage.SourceEventID, usage.Quantity.Amount().Coefficient(), usage.Quantity.Unit(), usage.Quantity.Amount().Scale(), usage.ContentDigest, usage.ObservedAt, usage.RecordedAt)
		if err != nil {
			return fmt.Errorf("insert trusted usage: %w", err)
		}
		if availableAfter == 0 {
			if err := applyBudgetExhaustion(ctx, repository.db, usage, runVersion, runUpdatedAt.UTC(), fencingToken, disposition); err != nil {
				return err
			}
		}
		receipt = domain.UsageReceipt{UsageID: usage.ID, AcceptedAt: usage.RecordedAt}
		return nil
	})
	return receipt, err
}

func applyBudgetExhaustion(ctx context.Context, db DBTX, usage domain.TrustedUsage, version int64, updatedAt time.Time, fencingToken int64, disposition domain.ExhaustionDisposition) error {
	current := domain.RunLifecycle{State: domain.RunRunning, Version: version, UpdatedAt: updatedAt}
	exhausted, _, err := current.Transition(domain.RunTransition{To: domain.RunBudgetExhausted, Actor: domain.RunActorGovernor, ExpectedVersion: version, At: usage.RecordedAt})
	if err != nil {
		return fmt.Errorf("transition exhausted run: %w", err)
	}
	target, actor := domain.RunFailedBudget, domain.RunActorSystem
	if disposition == domain.ExhaustionPause {
		target, actor = domain.RunPausedForBudget, domain.RunActorGovernor
	}
	final, _, err := exhausted.Transition(domain.RunTransition{To: target, Actor: actor, ExpectedVersion: exhausted.Version, At: usage.RecordedAt})
	if err != nil || fencingToken == math.MaxInt64 {
		return domain.NewError(domain.CodeConflict, "run cannot enter budget exhaustion")
	}
	result, err := db.Exec(ctx, `UPDATE runs SET state=$3,state_version=$4,updated_at=$5,terminal_at=$6,lease_id=NULL,lease_expires_at=NULL,fencing_token=$7 WHERE tenant_id=$1 AND id=$2 AND state='RUNNING' AND state_version=$8`, usage.TenantID.String(), usage.RunID.String(), string(final.State), final.Version, usage.RecordedAt, final.TerminalAt, fencingToken+1, version)
	if err != nil || result.RowsAffected() != 1 {
		return fmt.Errorf("persist budget exhaustion: %w", err)
	}
	firstID, err := domain.NewID()
	if err != nil {
		return err
	}
	secondID, err := domain.NewID()
	if err != nil {
		return err
	}
	auditID, err := domain.NewID()
	if err != nil {
		return err
	}
	var sequence int64
	if err := db.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM run_events WHERE tenant_id=$1 AND run_id=$2`, usage.TenantID.String(), usage.RunID.String()).Scan(&sequence); err != nil {
		return fmt.Errorf("sequence budget exhaustion events: %w", err)
	}
	for index, event := range []struct {
		id        domain.ID
		eventType string
		actor     domain.RunActor
		state     domain.RunState
		version   int64
	}{{firstID, "run.budget_exhausted", domain.RunActorGovernor, exhausted.State, exhausted.Version}, {secondID, budgetDispositionEvent(disposition), actor, final.State, final.Version}} {
		payload, _ := json.Marshal(map[string]any{"resource_name": usage.ResourceName, "state": event.state, "state_version": event.version, "usage_id": usage.ID.String()})
		_, err = db.Exec(ctx, `WITH inserted_event AS (INSERT INTO run_events(id,tenant_id,run_id,sequence,event_type,actor_type,actor_id,state,state_version,payload,occurred_at) VALUES($1,$2,$3,$4,$5,$6,NULL,$7,$8,$9,$10) RETURNING id) INSERT INTO outbox_messages(id,tenant_id,aggregate_type,aggregate_id,event_type,schema_version,payload,headers,occurred_at,available_at) SELECT id,$2,'run',$3::text,$5,1,$9,'{}'::jsonb,$10,$10 FROM inserted_event`, event.id.String(), usage.TenantID.String(), usage.RunID.String(), sequence+int64(index), event.eventType, string(event.actor), string(event.state), event.version, payload, usage.RecordedAt)
		if err != nil {
			return fmt.Errorf("append budget exhaustion event: %w", err)
		}
	}
	reasons := json.RawMessage(`["resource.budget.exhausted"]`)
	metadata, _ := json.Marshal(map[string]any{"disposition": disposition, "resource_name": usage.ResourceName, "state": final.State, "state_version": final.Version, "usage_id": usage.ID.String()})
	tenant, producer := usage.TenantID, usage.ProducerID
	audit := AuditEvent{ID: auditID, TenantID: &tenant, PrincipalID: &producer, Action: "resources.exhaust", ResourceType: "run", ResourceID: usage.RunID.String(), Outcome: "SUCCEEDED", ReasonCodes: reasons, Metadata: metadata, OccurredAt: usage.RecordedAt}
	eventHash, err := hashAuditEvent(audit)
	if err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `INSERT INTO audit_events(id,tenant_id,principal_id,action,resource_type,resource_id,outcome,reason_codes,metadata,event_hash,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, audit.ID.String(), tenant.String(), producer.String(), audit.Action, audit.ResourceType, audit.ResourceID, audit.Outcome, []byte(audit.ReasonCodes), []byte(audit.Metadata), eventHash, audit.OccurredAt); err != nil {
		return fmt.Errorf("append budget exhaustion audit: %w", err)
	}
	return nil
}

func budgetDispositionEvent(disposition domain.ExhaustionDisposition) string {
	if disposition == domain.ExhaustionPause {
		return "run.paused_for_budget"
	}
	return "run.failed_budget"
}
