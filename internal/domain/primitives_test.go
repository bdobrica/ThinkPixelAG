package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestIDRoundTripAndVersion(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 30, 0, 123000000, time.UTC)
	id, err := newID(now, bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	if got := id.String(); got != "019feba6-b9bb-7fff-bfff-ffffffffffff" {
		t.Fatalf("ID = %q", got)
	}
	parsed, err := ParseID(id.String())
	if err != nil || parsed != id {
		t.Fatalf("round trip = %v, %v", parsed, err)
	}
	encoded, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ID
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != id {
		t.Fatalf("JSON round trip = %v, %v", decoded, err)
	}
}

func TestIDRejectsInvalidInputsAndRandomnessFailure(t *testing.T) {
	invalid := []string{"", "01989985-f17b-6fff-bfff-ffffffffffff", "01989985-f17b-7fff-7fff-ffffffffffff", "01989985Ff17b-7fff-bfff-ffffffffffff"}
	for _, value := range invalid {
		if _, err := ParseID(value); !errors.Is(err, ErrInvalidID) {
			t.Errorf("ParseID(%q) error = %v", value, err)
		}
	}
	if _, err := newID(time.Now(), bytes.NewReader(nil)); err == nil {
		t.Fatal("expected entropy error")
	}
	if _, err := json.Marshal(ID{}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("zero marshal error = %v", err)
	}
}

func FuzzParseID(f *testing.F) {
	f.Add("01989985-f17b-7fff-bfff-ffffffffffff")
	f.Add("not-an-id")
	f.Fuzz(func(t *testing.T, value string) {
		id, err := ParseID(value)
		if err == nil {
			if reparsed, second := ParseID(id.String()); second != nil || reparsed != id {
				t.Fatalf("unstable successful parse")
			}
		}
	})
}

func TestUTCClock(t *testing.T) {
	now := SystemClock{}.Now()
	if now.Location() != time.UTC {
		t.Fatalf("location = %v", now.Location())
	}
	value := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	if got, err := RequireUTC(value); err != nil || !got.Equal(value) {
		t.Fatalf("RequireUTC = %v, %v", got, err)
	}
	if _, err := RequireUTC(value.In(time.FixedZone("UTC-alias", 0))); !errors.Is(err, ErrTimestampNotUTC) {
		t.Fatalf("alias error = %v", err)
	}
	if _, err := RequireUTC(value.In(time.FixedZone("EEST", 3*60*60))); !errors.Is(err, ErrTimestampNotUTC) {
		t.Fatalf("offset error = %v", err)
	}
}

func TestDecimalParsingFormattingAndJSON(t *testing.T) {
	cases := []string{"0", "1", "1.000000", "-12.34", "0.000000000000000001", "9223372036854775807", "-9223372036854775808"}
	for _, value := range cases {
		decimal, err := ParseDecimal(value)
		if err != nil {
			t.Fatalf("ParseDecimal(%q): %v", value, err)
		}
		if got := decimal.String(); got != value {
			t.Errorf("round trip %q = %q", value, got)
		}
		data, err := json.Marshal(decimal)
		if err != nil {
			t.Fatal(err)
		}
		var decoded Decimal
		if err := json.Unmarshal(data, &decoded); err != nil || decoded != decimal {
			t.Errorf("JSON round trip %q = %v, %v", value, decoded, err)
		}
	}
	for _, value := range []string{"", "+1", " 1", "01", "-0", ".1", "1.", "1e2", "1.0000000000000000000", "9223372036854775808"} {
		if _, err := ParseDecimal(value); !errors.Is(err, ErrInvalidDecimal) {
			t.Errorf("ParseDecimal(%q) error = %v", value, err)
		}
	}
}

func TestCheckedDecimalAndQuantityArithmetic(t *testing.T) {
	one, _ := NewDecimal(100, 2)
	half, _ := NewDecimal(50, 2)
	sum, err := one.Add(half)
	if err != nil || sum.String() != "1.50" {
		t.Fatalf("sum = %v, %v", sum, err)
	}
	difference, err := one.Sub(half)
	if err != nil || difference.String() != "0.50" {
		t.Fatalf("difference = %v, %v", difference, err)
	}
	if _, err := one.Add(Decimal{coefficient: 1, scale: 0}); !errors.Is(err, ErrScaleMismatch) {
		t.Fatalf("scale error = %v", err)
	}
	if _, err := (Decimal{coefficient: math.MaxInt64}).Add(Decimal{coefficient: 1}); !errors.Is(err, ErrDecimalOverflow) {
		t.Fatalf("overflow = %v", err)
	}
	if _, err := one.Sub(Decimal{coefficient: math.MinInt64, scale: 2}); !errors.Is(err, ErrDecimalOverflow) {
		t.Fatalf("sub overflow = %v", err)
	}
	quantity, err := NewQuantity(one, "usd_microunits")
	if err != nil {
		t.Fatal(err)
	}
	halfQuantity, _ := NewQuantity(half, quantity.Unit())
	added, err := quantity.Add(halfQuantity)
	if err != nil || added.Amount().String() != "1.50" {
		t.Fatalf("quantity sum = %v, %v", added, err)
	}
	over, _ := NewQuantity(Decimal{coefficient: 101, scale: 2}, quantity.Unit())
	if _, err := quantity.Sub(over); !errors.Is(err, ErrQuantityUnderflow) {
		t.Fatal("expected quantity underflow")
	}
	if _, err := NewQuantity(one, "USD"); err == nil {
		t.Fatal("expected invalid unit")
	}
	other, _ := NewQuantity(half, "llm_tokens")
	if _, err := quantity.Add(other); !errors.Is(err, ErrUnitMismatch) {
		t.Fatalf("unit error = %v", err)
	}
	encoded, err := json.Marshal(quantity)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Quantity
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded != quantity {
		t.Fatalf("quantity JSON = %v, %v", decoded, err)
	}
	if err := json.Unmarshal([]byte(`{"amount":"1.00","unit":"USD"}`), &decoded); err == nil {
		t.Fatal("expected invalid JSON quantity")
	}
	if _, err := json.Marshal(Quantity{}); err == nil {
		t.Fatal("expected invalid zero quantity")
	}
}

func FuzzParseDecimal(f *testing.F) {
	f.Add("12.3400")
	f.Add("invalid")
	f.Fuzz(func(t *testing.T, value string) {
		decimal, err := ParseDecimal(value)
		if err == nil {
			parsed, second := ParseDecimal(decimal.String())
			if second != nil || parsed != decimal {
				t.Fatalf("unstable parse for %q", value)
			}
		}
	})
}

func TestCursorRoundTripTamperAndLimits(t *testing.T) {
	key := bytes.Repeat([]byte("k"), 32)
	codec, err := NewCursorCodec(key)
	if err != nil {
		t.Fatal(err)
	}
	key[0] = 'x' // constructor must retain its own key copy
	id, _ := ParseID("01989985-f17b-7fff-bfff-ffffffffffff")
	encoded, err := codec.Encode(PageCursor{SortKey: "2026-08-10T12:30:00Z", ID: id})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.Decode(encoded)
	if err != nil || decoded.ID != id || decoded.SortKey != "2026-08-10T12:30:00Z" {
		t.Fatalf("decode = %v, %v", decoded, err)
	}
	tampered := encoded[:len(encoded)-1] + "A"
	if _, err := codec.Decode(tampered); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("tamper error = %v", err)
	}
	if _, err := NewCursorCodec([]byte("short")); err == nil {
		t.Fatal("expected short-key error")
	}
	if _, err := codec.Encode(PageCursor{SortKey: strings.Repeat("x", 257), ID: id}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("limit error = %v", err)
	}
}

func FuzzCursorDecode(f *testing.F) {
	codec, _ := NewCursorCodec(bytes.Repeat([]byte("k"), 32))
	f.Add("invalid")
	f.Add("")
	f.Fuzz(func(t *testing.T, value string) {
		cursor, err := codec.Decode(value)
		if err == nil {
			encoded, encodeErr := codec.Encode(cursor)
			if encodeErr != nil {
				t.Fatalf("decoded cursor cannot encode: %v", encodeErr)
			}
			if _, decodeErr := codec.Decode(encoded); decodeErr != nil {
				t.Fatal(decodeErr)
			}
		}
	})
}

func TestTypedErrors(t *testing.T) {
	cause := errors.New("database secret detail")
	err := WrapError(CodeUnavailable, "service temporarily unavailable", cause).WithRetryable()
	if got := err.Error(); strings.Contains(got, cause.Error()) || got != "unavailable: service temporarily unavailable" {
		t.Fatalf("public error = %q", got)
	}
	if !errors.Is(err, cause) || ErrorCodeOf(err) != CodeUnavailable {
		t.Fatal("cause or code unavailable")
	}
	if ErrorCodeOf(errors.New("unknown")) != CodeInternal {
		t.Fatal("untyped errors must map to internal")
	}
	if !err.Retryable() || err.Code() != CodeUnavailable || err.Detail() != "service temporarily unavailable" {
		t.Fatal("typed error accessors")
	}
	invalid := NewError("made_up", "unsafe")
	if ErrorCodeOf(invalid) != CodeInternal || strings.Contains(invalid.Error(), "unsafe") {
		t.Fatal("unknown codes must map to internal")
	}
}
