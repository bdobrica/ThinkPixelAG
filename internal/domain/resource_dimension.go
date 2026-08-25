package domain

import (
	"errors"
	"fmt"
	"regexp"
)

const DeadlineUTCUnixMicrosecondsUnit = "unix_microseconds_utc"

var (
	ErrInvalidResourceDimension = errors.New("invalid resource dimension")
	ErrResourceQuantityRange    = errors.New("resource quantity outside dimension bounds")

	resourceDimensionNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
)

// ResourceClass determines how governance enforces a resource dimension.
// Values are persisted, so adding a class requires a forward database migration.
type ResourceClass string

const (
	ResourceConsumable ResourceClass = "CONSUMABLE"
	ResourceStructural ResourceClass = "STRUCTURAL"
	ResourceDeadline   ResourceClass = "DEADLINE"
)

func (class ResourceClass) Valid() bool {
	switch class {
	case ResourceConsumable, ResourceStructural, ResourceDeadline:
		return true
	default:
		return false
	}
}

// ResourceAggregation states how values compose when authority is delegated.
type ResourceAggregation string

const (
	ResourceSum      ResourceAggregation = "SUM"
	ResourceMaximum  ResourceAggregation = "MAX"
	ResourceMinimum  ResourceAggregation = "MIN"
	ResourceAbsolute ResourceAggregation = "ABSOLUTE"
)

func (aggregation ResourceAggregation) Valid() bool {
	switch aggregation {
	case ResourceSum, ResourceMaximum, ResourceMinimum, ResourceAbsolute:
		return true
	default:
		return false
	}
}

// ResourceDimension is a tenant-scoped definition. Amounts are exact signed
// 64-bit coefficients interpreted at Scale decimal places in Unit.
type ResourceDimension struct {
	ID          ID
	TenantID    ID
	Name        string
	Class       ResourceClass
	Unit        string
	Scale       uint8
	Minimum     int64
	Maximum     int64
	Aggregation ResourceAggregation
}

func (dimension ResourceDimension) Validate() error {
	if dimension.ID.IsZero() || dimension.TenantID.IsZero() {
		return fmt.Errorf("%w: identifiers must be set", ErrInvalidResourceDimension)
	}
	if !resourceDimensionNamePattern.MatchString(dimension.Name) {
		return fmt.Errorf("%w: name must be canonical", ErrInvalidResourceDimension)
	}
	if !dimension.Class.Valid() || !dimension.Aggregation.Valid() {
		return fmt.Errorf("%w: class or aggregation is unknown", ErrInvalidResourceDimension)
	}
	if !validUnit(dimension.Unit) {
		return fmt.Errorf("%w: unit must be canonical", ErrInvalidResourceDimension)
	}
	if dimension.Scale > MaxDecimalScale || dimension.Minimum < 0 || dimension.Maximum < dimension.Minimum {
		return fmt.Errorf("%w: numeric bounds are invalid", ErrInvalidResourceDimension)
	}

	switch dimension.Class {
	case ResourceConsumable:
		if dimension.Aggregation != ResourceSum {
			return fmt.Errorf("%w: consumables require SUM aggregation", ErrInvalidResourceDimension)
		}
	case ResourceStructural:
		if dimension.Aggregation != ResourceMaximum && dimension.Aggregation != ResourceMinimum {
			return fmt.Errorf("%w: structural dimensions require MAX or MIN aggregation", ErrInvalidResourceDimension)
		}
	case ResourceDeadline:
		if dimension.Aggregation != ResourceAbsolute || dimension.Unit != DeadlineUTCUnixMicrosecondsUnit || dimension.Scale != 0 {
			return fmt.Errorf("%w: deadlines require an absolute UTC microsecond instant", ErrInvalidResourceDimension)
		}
	}
	return nil
}

// ValidateQuantity rejects unit, scale, and range mismatches before a resource
// value reaches persistence or arithmetic code.
func (dimension ResourceDimension) ValidateQuantity(quantity Quantity) error {
	if err := dimension.Validate(); err != nil {
		return err
	}
	if _, err := NewQuantity(quantity.Amount(), quantity.Unit()); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResourceDimension, err)
	}
	if quantity.Unit() != dimension.Unit || quantity.Amount().Scale() != dimension.Scale {
		return ErrUnitMismatch
	}
	coefficient := quantity.Amount().Coefficient()
	if coefficient < dimension.Minimum || coefficient > dimension.Maximum {
		return ErrResourceQuantityRange
	}
	return nil
}
