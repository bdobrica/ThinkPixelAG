package domain

import (
	"errors"
	"testing"
	"time"
)

func TestThroughputDimensionName(t *testing.T) {
	if got, err := ThroughputDimensionName("tool_calls"); err != nil || got != "tool_calls_per_minute" {
		t.Fatalf("name=%q err=%v", got, err)
	}
	if _, err := ThroughputDimensionName("Bad Name"); !errors.Is(err, ErrInvalidTrustedUsage) {
		t.Fatalf("error=%v", err)
	}
	long := "a123456789012345678901234567890123456789012345678901234"
	if _, err := ThroughputDimensionName(long); !errors.Is(err, ErrInvalidTrustedUsage) {
		t.Fatalf("long name error=%v", err)
	}
	if got, err := ThroughputUnitName("calls"); err != nil || got != "calls_per_minute" {
		t.Fatalf("unit=%q err=%v", got, err)
	}
	if _, err := ThroughputUnitName("Bad Unit"); !errors.Is(err, ErrInvalidTrustedUsage) {
		t.Fatalf("unit error=%v", err)
	}
	limit := &ThroughputLimitExceeded{DimensionName: "tool_calls_per_minute", RetryAt: time.Now()}
	if !errors.Is(limit, ErrStructuralThroughputExceeded) {
		t.Fatal("typed limit must unwrap to sentinel")
	}
}
