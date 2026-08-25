package domain

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var ErrInvalidResourceReservation = errors.New("invalid resource reservation")

// ResourceReservationAmount is one exact coefficient delegated from a parent
// envelope. Unit and scale are copied from the authoritative parent grant.
type ResourceReservationAmount struct {
	DimensionID ID
	Coefficient int64
}

// ResourceReservation describes an open, all-or-nothing delegation from a
// parent envelope to an already-created child envelope and run.
type ResourceReservation struct {
	ID, TenantID, ParentEnvelopeID, ChildEnvelopeID, ChildRunID ID
	Amounts                                                     []ResourceReservationAmount
	ExpiresAt                                                   *time.Time
	CreatedAt                                                   time.Time
}

// ValidateAndSortResourceReservation validates the transaction command and
// returns a defensive copy ordered by dimension UUID. Persistence locks rows
// in this order so callers cannot introduce lock-order inversions.
func ValidateAndSortResourceReservation(reservation ResourceReservation) (ResourceReservation, error) {
	if reservation.ID.IsZero() || reservation.TenantID.IsZero() || reservation.ParentEnvelopeID.IsZero() || reservation.ChildEnvelopeID.IsZero() || reservation.ChildRunID.IsZero() {
		return ResourceReservation{}, fmt.Errorf("%w: identifiers must be set", ErrInvalidResourceReservation)
	}
	if reservation.ParentEnvelopeID == reservation.ChildEnvelopeID || reservation.CreatedAt.IsZero() || len(reservation.Amounts) == 0 {
		return ResourceReservation{}, fmt.Errorf("%w: parent, time, and amounts are invalid", ErrInvalidResourceReservation)
	}
	if reservation.ExpiresAt != nil && !reservation.ExpiresAt.After(reservation.CreatedAt) {
		return ResourceReservation{}, fmt.Errorf("%w: expiry must follow creation", ErrInvalidResourceReservation)
	}

	result := reservation
	result.Amounts = append([]ResourceReservationAmount(nil), reservation.Amounts...)
	sort.Slice(result.Amounts, func(i, j int) bool {
		return result.Amounts[i].DimensionID.String() < result.Amounts[j].DimensionID.String()
	})
	for index, amount := range result.Amounts {
		if amount.DimensionID.IsZero() || amount.Coefficient <= 0 {
			return ResourceReservation{}, fmt.Errorf("%w: amounts must be positive and identified", ErrInvalidResourceReservation)
		}
		if index > 0 && amount.DimensionID == result.Amounts[index-1].DimensionID {
			return ResourceReservation{}, fmt.Errorf("%w: dimensions must be unique", ErrInvalidResourceReservation)
		}
	}
	return result, nil
}
