package domain

import (
	"errors"
	"testing"
	"time"
)

func TestResourceSettlementValidation(t *testing.T) {
	ids := make([]ID, 6)
	for i := range ids {
		ids[i], _ = NewID()
	}
	candidate := ResourceSettlement{ID: ids[0], TenantID: ids[1], ReservationID: ids[2], ActorPrincipalID: ids[3], PolicyDecisionID: ids[4], IdempotencyKey: "settle-1", TerminalRunState: string(RunCompleted), FinalUsageEventIDs: []ID{ids[5]}, SettledAt: time.Unix(10, 0).UTC()}
	if _, err := ValidateResourceSettlement(candidate); err != nil {
		t.Fatal(err)
	}
	candidate.TerminalRunState = string(RunRunning)
	if _, err := ValidateResourceSettlement(candidate); !errors.Is(err, ErrInvalidResourceSettlement) {
		t.Fatalf("error=%v", err)
	}
}
