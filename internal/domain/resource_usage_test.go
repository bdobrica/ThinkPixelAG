package domain

import (
	"testing"
	"time"
)

func TestTrustedUsageValidationAndDigest(t *testing.T) {
	ids := make([]ID, 4)
	for i := range ids {
		ids[i], _ = NewID()
	}
	amount, _ := NewDecimal(3, 0)
	quantity, _ := NewQuantity(amount, "llm_tokens")
	usage := TrustedUsage{ID: ids[0], TenantID: ids[1], RunID: ids[2], ProducerID: ids[3], SourceEventID: "meter-1", ResourceName: "llm_tokens", Quantity: quantity, ObservedAt: time.Unix(10, 0).UTC(), RecordedAt: time.Unix(11, 0).UTC()}
	first, err := ValidateTrustedUsage(usage)
	second, _ := ValidateTrustedUsage(usage)
	if err != nil || first.ContentDigest != second.ContentDigest || !ValidDigest(first.ContentDigest) {
		t.Fatalf("usage=%+v err=%v", first, err)
	}
	negative, _ := NewDecimal(-1, 0)
	usage.Quantity = Quantity{amount: negative, unit: "llm_tokens"}
	if _, err := ValidateTrustedUsage(usage); err == nil {
		t.Fatal("negative usage accepted")
	}
	usage.Quantity = quantity
	usage.SourceEventID = " bad "
	if _, err := ValidateTrustedUsage(usage); err == nil {
		t.Fatal("invalid source event accepted")
	}
}
