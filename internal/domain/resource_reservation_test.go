package domain

import (
	"errors"
	"testing"
	"time"
)

func TestValidateAndSortResourceReservation(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	low := resourceTestID(t, "018f107d-3f5b-7a2c-8d11-111111111111")
	high := resourceTestID(t, "018f107d-3f5b-7a2c-8d11-ffffffffffff")
	reservation := ResourceReservation{
		ID: resourceTestID(t, "018f107d-3f5b-7a2c-8d11-222222222222"), TenantID: resourceTestID(t, "018f107d-3f5b-7a2c-8d11-333333333333"),
		ParentEnvelopeID: resourceTestID(t, "018f107d-3f5b-7a2c-8d11-444444444444"), ChildEnvelopeID: resourceTestID(t, "018f107d-3f5b-7a2c-8d11-555555555555"), ChildRunID: resourceTestID(t, "018f107d-3f5b-7a2c-8d11-666666666666"),
		Amounts: []ResourceReservationAmount{{DimensionID: high, Coefficient: 20}, {DimensionID: low, Coefficient: 10}}, CreatedAt: created,
	}

	got, err := ValidateAndSortResourceReservation(reservation)
	if err != nil {
		t.Fatal(err)
	}
	if got.Amounts[0].DimensionID != low || got.Amounts[1].DimensionID != high || reservation.Amounts[0].DimensionID != high {
		t.Fatalf("reservation was not defensively sorted: got=%+v input=%+v", got.Amounts, reservation.Amounts)
	}

	tests := []struct {
		name   string
		mutate func(*ResourceReservation)
	}{
		{"missing identifier", func(r *ResourceReservation) { r.ID = ID{} }},
		{"same envelope", func(r *ResourceReservation) { r.ChildEnvelopeID = r.ParentEnvelopeID }},
		{"no amounts", func(r *ResourceReservation) { r.Amounts = nil }},
		{"zero amount", func(r *ResourceReservation) { r.Amounts[0].Coefficient = 0 }},
		{"duplicate dimension", func(r *ResourceReservation) { r.Amounts[1].DimensionID = r.Amounts[0].DimensionID }},
		{"expired", func(r *ResourceReservation) { expiry := created; r.ExpiresAt = &expiry }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := reservation
			invalid.Amounts = append([]ResourceReservationAmount(nil), reservation.Amounts...)
			test.mutate(&invalid)
			if _, err := ValidateAndSortResourceReservation(invalid); !errors.Is(err, ErrInvalidResourceReservation) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
