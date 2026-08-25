package domain

import (
	"errors"
	"testing"
)

func TestResourceDimensionValidation(t *testing.T) {
	t.Parallel()
	id := resourceTestID(t, "018f107d-3f5b-7a2c-8d11-111111111111")
	tenantID := resourceTestID(t, "018f107d-3f5b-7a2c-8d11-222222222222")
	valid := ResourceDimension{
		ID: id, TenantID: tenantID, Name: "llm_tokens", Class: ResourceConsumable,
		Unit: "tokens", Scale: 0, Minimum: 0, Maximum: 1_000_000, Aggregation: ResourceSum,
	}

	tests := []struct {
		name      string
		mutate    func(*ResourceDimension)
		wantError bool
	}{
		{name: "consumable"},
		{name: "structural maximum", mutate: func(d *ResourceDimension) {
			d.Name, d.Class, d.Unit, d.Aggregation = "active_children", ResourceStructural, "children", ResourceMaximum
		}},
		{name: "structural minimum", mutate: func(d *ResourceDimension) {
			d.Name, d.Class, d.Unit, d.Aggregation = "calls_per_minute", ResourceStructural, "calls_per_minute", ResourceMinimum
		}},
		{name: "deadline", mutate: func(d *ResourceDimension) {
			d.Name, d.Class, d.Unit, d.Aggregation, d.Maximum = "run_deadline", ResourceDeadline, DeadlineUTCUnixMicrosecondsUnit, ResourceAbsolute, 9_000_000_000_000_000
		}},
		{name: "missing id", mutate: func(d *ResourceDimension) { d.ID = ID{} }, wantError: true},
		{name: "noncanonical name", mutate: func(d *ResourceDimension) { d.Name = "LLM-Tokens" }, wantError: true},
		{name: "noncanonical unit", mutate: func(d *ResourceDimension) { d.Unit = "Tokens" }, wantError: true},
		{name: "unknown class", mutate: func(d *ResourceDimension) { d.Class = "POOL" }, wantError: true},
		{name: "unknown aggregation", mutate: func(d *ResourceDimension) { d.Aggregation = "AVERAGE" }, wantError: true},
		{name: "negative minimum", mutate: func(d *ResourceDimension) { d.Minimum = -1 }, wantError: true},
		{name: "reversed bounds", mutate: func(d *ResourceDimension) { d.Minimum, d.Maximum = 2, 1 }, wantError: true},
		{name: "excess scale", mutate: func(d *ResourceDimension) { d.Scale = MaxDecimalScale + 1 }, wantError: true},
		{name: "consumable wrong aggregation", mutate: func(d *ResourceDimension) { d.Aggregation = ResourceMaximum }, wantError: true},
		{name: "structural sum", mutate: func(d *ResourceDimension) { d.Class = ResourceStructural }, wantError: true},
		{name: "deadline wrong unit", mutate: func(d *ResourceDimension) { d.Class, d.Aggregation = ResourceDeadline, ResourceAbsolute }, wantError: true},
		{name: "deadline fractional", mutate: func(d *ResourceDimension) {
			d.Class, d.Aggregation, d.Unit, d.Scale = ResourceDeadline, ResourceAbsolute, DeadlineUTCUnixMicrosecondsUnit, 1
		}, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dimension := valid
			if test.mutate != nil {
				test.mutate(&dimension)
			}
			err := dimension.Validate()
			if (err != nil) != test.wantError {
				t.Fatalf("Validate() error = %v, wantError %v", err, test.wantError)
			}
			if test.wantError && !errors.Is(err, ErrInvalidResourceDimension) {
				t.Fatalf("error = %v, want invalid dimension", err)
			}
		})
	}
}

func TestResourceDimensionValidatesTypedQuantity(t *testing.T) {
	t.Parallel()
	dimension := ResourceDimension{
		ID: resourceTestID(t, "018f107d-3f5b-7a2c-8d11-111111111111"), TenantID: resourceTestID(t, "018f107d-3f5b-7a2c-8d11-222222222222"),
		Name: "usd_budget", Class: ResourceConsumable, Unit: "usd_microunits", Scale: 0,
		Minimum: 100, Maximum: 1_000, Aggregation: ResourceSum,
	}
	quantity := func(value int64, scale uint8, unit string) Quantity {
		decimal, err := NewDecimal(value, scale)
		if err != nil {
			t.Fatal(err)
		}
		result, err := NewQuantity(decimal, unit)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if err := dimension.ValidateQuantity(quantity(500, 0, "usd_microunits")); err != nil {
		t.Fatalf("valid quantity: %v", err)
	}
	if err := dimension.ValidateQuantity(quantity(99, 0, "usd_microunits")); !errors.Is(err, ErrResourceQuantityRange) {
		t.Fatalf("below minimum = %v", err)
	}
	if err := dimension.ValidateQuantity(quantity(1_001, 0, "usd_microunits")); !errors.Is(err, ErrResourceQuantityRange) {
		t.Fatalf("above maximum = %v", err)
	}
	if err := dimension.ValidateQuantity(quantity(500, 0, "tokens")); !errors.Is(err, ErrUnitMismatch) {
		t.Fatalf("unit mismatch = %v", err)
	}
	if err := dimension.ValidateQuantity(quantity(500, 1, "usd_microunits")); !errors.Is(err, ErrUnitMismatch) {
		t.Fatalf("scale mismatch = %v", err)
	}
}

func resourceTestID(t *testing.T, value string) ID {
	t.Helper()
	id, err := ParseID(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
