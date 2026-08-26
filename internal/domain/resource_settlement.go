package domain

import (
	"errors"
	"sort"
	"time"
)

var ErrInvalidResourceSettlement = errors.New("invalid resource settlement")

type ResourceSettlement struct {
	ID, TenantID, ReservationID, ActorPrincipalID, PolicyDecisionID ID
	IdempotencyKey, TerminalRunState                                string
	FinalUsageEventIDs                                              []ID
	SettledAt                                                       time.Time
}

type ResourceSettlementAmount struct {
	Name, Unit string
	Scale      uint8
	Value      int64
}

type ResourceSettlementResult struct {
	ID, ReservationID  ID
	Consumed, Returned []ResourceSettlementAmount
	SettledAt          time.Time
	Duplicate          bool
}

func ValidateResourceSettlement(candidate ResourceSettlement) (ResourceSettlement, error) {
	state := RunState(candidate.TerminalRunState)
	if candidate.ID.IsZero() || candidate.TenantID.IsZero() || candidate.ReservationID.IsZero() || candidate.ActorPrincipalID.IsZero() || candidate.PolicyDecisionID.IsZero() ||
		!extensionReferencePattern.MatchString(candidate.IdempotencyKey) || !state.Terminal() || state == RunBudgetExhausted || len(candidate.FinalUsageEventIDs) > 10000 {
		return ResourceSettlement{}, ErrInvalidResourceSettlement
	}
	settledAt, err := RequireUTC(candidate.SettledAt)
	if err != nil || settledAt.IsZero() {
		return ResourceSettlement{}, ErrInvalidResourceSettlement
	}
	candidate.SettledAt = settledAt
	candidate.FinalUsageEventIDs = append([]ID(nil), candidate.FinalUsageEventIDs...)
	sort.Slice(candidate.FinalUsageEventIDs, func(i, j int) bool {
		return candidate.FinalUsageEventIDs[i].String() < candidate.FinalUsageEventIDs[j].String()
	})
	for i, id := range candidate.FinalUsageEventIDs {
		if id.IsZero() || (i > 0 && id == candidate.FinalUsageEventIDs[i-1]) {
			return ResourceSettlement{}, ErrInvalidResourceSettlement
		}
	}
	return candidate, nil
}
