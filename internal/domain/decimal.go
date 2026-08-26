package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const MaxDecimalScale = 18

var (
	ErrInvalidDecimal    = errors.New("invalid decimal")
	ErrDecimalOverflow   = errors.New("decimal arithmetic overflow")
	ErrScaleMismatch     = errors.New("decimal scales differ")
	ErrUnitMismatch      = errors.New("resource quantity units differ")
	ErrQuantityUnderflow = errors.New("resource quantity underflow")
)

// Decimal is an exact signed fixed-point value: coefficient / 10^scale.
// JSON uses strings so transport layers never introduce floating-point loss.
type Decimal struct {
	coefficient int64
	scale       uint8
}

func NewDecimal(coefficient int64, scale uint8) (Decimal, error) {
	if scale > MaxDecimalScale {
		return Decimal{}, ErrInvalidDecimal
	}
	return Decimal{coefficient: coefficient, scale: scale}, nil
}

func ParseDecimal(value string) (Decimal, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.HasPrefix(value, "+") {
		return Decimal{}, ErrInvalidDecimal
	}
	negative := strings.HasPrefix(value, "-")
	digits := value
	if negative {
		digits = digits[1:]
	}
	parts := strings.Split(digits, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") || len(parts) == 2 && len(parts[1]) > MaxDecimalScale {
		return Decimal{}, ErrInvalidDecimal
	}
	all := strings.Join(parts, "")
	for _, char := range all {
		if char < '0' || char > '9' {
			return Decimal{}, ErrInvalidDecimal
		}
	}
	if len(parts[0]) > 1 && parts[0][0] == '0' || negative && all == strings.Repeat("0", len(all)) {
		return Decimal{}, ErrInvalidDecimal
	}
	if negative {
		all = "-" + all
	}
	coefficient, err := strconv.ParseInt(all, 10, 64)
	if err != nil {
		return Decimal{}, ErrInvalidDecimal
	}
	scale := uint8(0)
	if len(parts) == 2 {
		scale = uint8(len(parts[1]))
	}
	return Decimal{coefficient: coefficient, scale: scale}, nil
}

func (d Decimal) Coefficient() int64 { return d.coefficient }
func (d Decimal) Scale() uint8       { return d.scale }
func (d Decimal) IsNegative() bool   { return d.coefficient < 0 }

func (d Decimal) Add(other Decimal) (Decimal, error) {
	if d.scale != other.scale {
		return Decimal{}, ErrScaleMismatch
	}
	if other.coefficient > 0 && d.coefficient > math.MaxInt64-other.coefficient || other.coefficient < 0 && d.coefficient < math.MinInt64-other.coefficient {
		return Decimal{}, ErrDecimalOverflow
	}
	return Decimal{coefficient: d.coefficient + other.coefficient, scale: d.scale}, nil
}

func (d Decimal) Sub(other Decimal) (Decimal, error) {
	if d.scale != other.scale {
		return Decimal{}, ErrScaleMismatch
	}
	if other.coefficient > 0 && d.coefficient < math.MinInt64+other.coefficient || other.coefficient < 0 && d.coefficient > math.MaxInt64+other.coefficient {
		return Decimal{}, ErrDecimalOverflow
	}
	return Decimal{coefficient: d.coefficient - other.coefficient, scale: d.scale}, nil
}

func (d Decimal) String() string {
	if d.scale == 0 {
		return strconv.FormatInt(d.coefficient, 10)
	}
	negative := d.coefficient < 0
	var magnitude uint64
	if negative {
		magnitude = uint64(-(d.coefficient + 1)) + 1
	} else {
		magnitude = uint64(d.coefficient)
	}
	digits := strconv.FormatUint(magnitude, 10)
	for len(digits) <= int(d.scale) {
		digits = "0" + digits
	}
	cut := len(digits) - int(d.scale)
	if negative {
		return "-" + digits[:cut] + "." + digits[cut:]
	}
	return digits[:cut] + "." + digits[cut:]
}

func (d Decimal) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }
func (d *Decimal) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return ErrInvalidDecimal
	}
	parsed, err := ParseDecimal(value)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Quantity couples an exact nonnegative amount to a bounded canonical unit.
type Quantity struct {
	amount Decimal
	unit   string
}

func NewQuantity(amount Decimal, unit string) (Quantity, error) {
	if amount.IsNegative() || !validUnit(unit) {
		return Quantity{}, fmt.Errorf("%w: amount must be nonnegative and unit canonical", ErrInvalidDecimal)
	}
	return Quantity{amount: amount, unit: unit}, nil
}

func (q Quantity) Amount() Decimal { return q.amount }
func (q Quantity) Unit() string    { return q.unit }

func validUnit(unit string) bool {
	if len(unit) < 1 || len(unit) > 63 {
		return false
	}
	for index, char := range unit {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' && index > 0 || char == '_' && index > 0 {
			continue
		}
		return false
	}
	return true
}

func (q Quantity) Add(other Quantity) (Quantity, error) {
	if q.unit != other.unit {
		return Quantity{}, ErrUnitMismatch
	}
	amount, err := q.amount.Add(other.amount)
	if err != nil {
		return Quantity{}, err
	}
	return NewQuantity(amount, q.unit)
}

func (q Quantity) Sub(other Quantity) (Quantity, error) {
	if q.unit != other.unit {
		return Quantity{}, ErrUnitMismatch
	}
	amount, err := q.amount.Sub(other.amount)
	if err != nil {
		return Quantity{}, err
	}
	if amount.IsNegative() {
		return Quantity{}, ErrQuantityUnderflow
	}
	return NewQuantity(amount, q.unit)
}

func (q Quantity) MarshalJSON() ([]byte, error) {
	if _, err := NewQuantity(q.amount, q.unit); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Amount Decimal `json:"amount"`
		Unit   string  `json:"unit"`
	}{q.amount, q.unit})
}

func (q *Quantity) UnmarshalJSON(data []byte) error {
	var raw struct {
		Amount Decimal `json:"amount"`
		Unit   string  `json:"unit"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := NewQuantity(raw.Amount, raw.Unit)
	if err != nil {
		return err
	}
	*q = parsed
	return nil
}
