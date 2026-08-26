package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.TrustedUsageRepository = (*TenantRepository)(nil)

func (r *TenantRepository) RecordTrustedUsage(ctx context.Context, candidate domain.TrustedUsage) (receipt domain.UsageReceipt, err error) {
	if err = r.valid(); err != nil {
		return receipt, err
	}
	if candidate.TenantID != r.tenantID {
		return receipt, errors.New("trusted usage does not match repository scope")
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
		var envelopeID, dimensionID, class, unit string
		var scale int16
		err = repository.db.QueryRow(ctx, `SELECT e.id::text,d.id::text,d.class,g.unit,g.scale FROM resource_envelopes e JOIN resource_envelope_grants g ON g.tenant_id=e.tenant_id AND g.envelope_id=e.id JOIN resource_dimensions d ON d.tenant_id=g.tenant_id AND d.id=g.dimension_id JOIN principals p ON p.tenant_id=e.tenant_id AND p.id=$3 WHERE e.tenant_id=$1 AND e.run_id=$2 AND d.name=$4 AND p.disabled_at IS NULL FOR UPDATE OF e`, r.tenantID.String(), usage.RunID.String(), usage.ProducerID.String(), usage.ResourceName).Scan(&envelopeID, &dimensionID, &class, &unit, &scale)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeNotFound, "metering target not found")
		}
		if err != nil {
			return fmt.Errorf("resolve trusted usage target: %w", err)
		}
		if class != string(domain.ResourceConsumable) || unit != usage.Quantity.Unit() || scale != int16(usage.Quantity.Amount().Scale()) {
			return domain.NewError(domain.CodeInvalidArgument, "trusted usage resource or unit is invalid")
		}
		var closed bool
		if err := repository.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM resource_reservations WHERE tenant_id=$1 AND child_envelope_id=$2 AND state<>'OPEN')`, r.tenantID.String(), envelopeID).Scan(&closed); err != nil {
			return fmt.Errorf("check usage settlement: %w", err)
		}
		if closed {
			return domain.NewError(domain.CodeConflict, "late usage is not accepted after settlement")
		}
		tag, err := repository.db.Exec(ctx, `UPDATE resource_balances SET available_value=available_value-$4,direct_consumed_value=direct_consumed_value+$4,state_version=state_version+1,updated_at=$5 WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3 AND available_value >= $4 AND direct_consumed_value <= 9223372036854775807-$4 AND state_version < 9223372036854775807`, r.tenantID.String(), envelopeID, dimensionID, usage.Quantity.Amount().Coefficient(), usage.RecordedAt)
		if err != nil {
			return fmt.Errorf("apply trusted usage: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return domain.NewError(domain.CodeConflict, "trusted usage exceeds available resource")
		}
		_, err = repository.db.Exec(ctx, `INSERT INTO trusted_usage_entries(id,tenant_id,run_id,envelope_id,dimension_id,producer_id,source_event_id,quantity_value,unit,scale,content_digest,observed_at,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, usage.ID.String(), r.tenantID.String(), usage.RunID.String(), envelopeID, dimensionID, usage.ProducerID.String(), usage.SourceEventID, usage.Quantity.Amount().Coefficient(), usage.Quantity.Unit(), usage.Quantity.Amount().Scale(), usage.ContentDigest, usage.ObservedAt, usage.RecordedAt)
		if err != nil {
			return fmt.Errorf("insert trusted usage: %w", err)
		}
		receipt = domain.UsageReceipt{UsageID: usage.ID, AcceptedAt: usage.RecordedAt}
		return nil
	})
	return receipt, err
}
