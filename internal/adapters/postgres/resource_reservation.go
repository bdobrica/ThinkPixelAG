package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.ResourceReservationRepository = (*TenantRepository)(nil)

type lockedResourceBalance struct {
	unit      string
	scale     int16
	available int64
}

// ReserveChildResources locks and debits every parent dimension in canonical
// UUID order. The transaction creates the reservation, immutable item vector,
// and initial child grants/balances together or leaves no mutation behind.
func (r *TenantRepository) ReserveChildResources(ctx context.Context, requested domain.ResourceReservation) (domain.ResourceReservation, error) {
	if err := r.valid(); err != nil {
		return domain.ResourceReservation{}, err
	}
	if requested.TenantID != r.tenantID {
		return domain.ResourceReservation{}, errors.New("resource reservation does not match repository scope")
	}
	reservation, err := domain.ValidateAndSortResourceReservation(requested)
	if err != nil {
		return domain.ResourceReservation{}, err
	}

	err = r.withResourceTransaction(ctx, func(repository *TenantRepository) error {
		var childEnvelope string
		if err := repository.db.QueryRow(ctx, `SELECT e.id::text FROM resource_envelopes e JOIN runs child ON child.tenant_id=e.tenant_id AND child.id=e.run_id WHERE e.tenant_id=$1 AND e.id=$2 AND e.run_id=$3 AND e.parent_envelope_id=$4 FOR UPDATE`, r.tenantID.String(), reservation.ChildEnvelopeID.String(), reservation.ChildRunID.String(), reservation.ParentEnvelopeID.String()).Scan(&childEnvelope); errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError(domain.CodeConflict, "child envelope is unavailable for reservation")
		} else if err != nil {
			return fmt.Errorf("lock child envelope: %w", err)
		}

		locked := make(map[domain.ID]lockedResourceBalance, len(reservation.Amounts))
		for _, amount := range reservation.Amounts {
			var balance lockedResourceBalance
			var class string
			err := repository.db.QueryRow(ctx, `SELECT d.class,g.unit,g.scale,b.available_value FROM resource_balances b JOIN resource_envelope_grants g USING(tenant_id,envelope_id,dimension_id) JOIN resource_dimensions d ON d.tenant_id=b.tenant_id AND d.id=b.dimension_id WHERE b.tenant_id=$1 AND b.envelope_id=$2 AND b.dimension_id=$3 FOR UPDATE OF b`, r.tenantID.String(), reservation.ParentEnvelopeID.String(), amount.DimensionID.String()).Scan(&class, &balance.unit, &balance.scale, &balance.available)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && class != string(domain.ResourceConsumable)) {
				return domain.NewError(domain.CodeConflict, "parent consumable balance is unavailable")
			}
			if err != nil {
				return fmt.Errorf("lock parent resource balance: %w", err)
			}
			if balance.available < amount.Coefficient {
				return domain.NewError(domain.CodeConflict, "parent resource availability is insufficient")
			}
			locked[amount.DimensionID] = balance
		}

		if _, err := repository.db.Exec(ctx, `INSERT INTO resource_reservations(id,tenant_id,parent_envelope_id,child_envelope_id,child_run_id,state,expires_at,created_at) VALUES($1,$2,$3,$4,$5,'OPEN',$6,$7)`, reservation.ID.String(), r.tenantID.String(), reservation.ParentEnvelopeID.String(), reservation.ChildEnvelopeID.String(), reservation.ChildRunID.String(), reservation.ExpiresAt, reservation.CreatedAt); err != nil {
			if ClassifyError(err) == ErrorUniqueViolation {
				return domain.WrapError(domain.CodeConflict, "resource reservation already exists", err)
			}
			return fmt.Errorf("insert resource reservation: %w", err)
		}

		for _, amount := range reservation.Amounts {
			balance := locked[amount.DimensionID]
			tag, err := repository.db.Exec(ctx, `UPDATE resource_balances SET available_value=available_value-$4,allocated_open_value=allocated_open_value+$4,state_version=state_version+1,updated_at=$5 WHERE tenant_id=$1 AND envelope_id=$2 AND dimension_id=$3 AND available_value >= $4 AND allocated_open_value <= 9223372036854775807-$4 AND state_version < 9223372036854775807`, r.tenantID.String(), reservation.ParentEnvelopeID.String(), amount.DimensionID.String(), amount.Coefficient, reservation.CreatedAt)
			if err != nil {
				return fmt.Errorf("debit parent resource balance: %w", err)
			}
			if tag.RowsAffected() != 1 {
				return domain.NewError(domain.CodeConflict, "parent resource balance changed or is exhausted")
			}
			if _, err := repository.db.Exec(ctx, `INSERT INTO resource_reservation_items(tenant_id,reservation_id,dimension_id,reserved_value,unit,scale) VALUES($1,$2,$3,$4,$5,$6)`, r.tenantID.String(), reservation.ID.String(), amount.DimensionID.String(), amount.Coefficient, balance.unit, balance.scale); err != nil {
				return fmt.Errorf("insert resource reservation item: %w", err)
			}
			if _, err := repository.db.Exec(ctx, `INSERT INTO resource_envelope_grants(tenant_id,envelope_id,dimension_id,granted_value,unit,scale) VALUES($1,$2,$3,$4,$5,$6)`, r.tenantID.String(), reservation.ChildEnvelopeID.String(), amount.DimensionID.String(), amount.Coefficient, balance.unit, balance.scale); err != nil {
				return fmt.Errorf("issue child resource grant: %w", err)
			}
			if _, err := repository.db.Exec(ctx, `INSERT INTO resource_balances(tenant_id,envelope_id,dimension_id,available_value,direct_consumed_value,allocated_open_value,state_version,updated_at) VALUES($1,$2,$3,$4,0,0,1,$5)`, r.tenantID.String(), reservation.ChildEnvelopeID.String(), amount.DimensionID.String(), amount.Coefficient, reservation.CreatedAt); err != nil {
				return fmt.Errorf("initialize child resource balance: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return domain.ResourceReservation{}, err
	}
	return reservation, nil
}

func (r *TenantRepository) withResourceTransaction(ctx context.Context, fn func(*TenantRepository) error) error {
	if beginner, ok := r.db.(interface {
		Begin(context.Context) (pgx.Tx, error)
	}); ok {
		tx, err := beginner.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin resource reservation: %w", err)
		}
		err = fn(&TenantRepository{db: tx, tenantID: r.tenantID})
		if err != nil {
			if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return errors.Join(err, rollbackErr)
			}
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit resource reservation: %w", err)
		}
		return nil
	}
	return fn(r)
}
