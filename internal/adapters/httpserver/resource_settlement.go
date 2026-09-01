package httpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

type ResourceSettlementApplication interface {
	Settle(context.Context, application.SettleReservation) (domain.ResourceSettlementResult, error)
}
type ResourceSettlementHTTPConfig struct{ SecurityState policy.SecurityState }
type settlementBody struct {
	TerminalRunState   string   `json:"terminal_run_state"`
	FinalUsageEventIDs []string `json:"final_usage_event_ids"`
}

func ResourceSettlementHandler(verifier WorkloadVerifier, service ResourceSettlementApplication, config ResourceSettlementHTTPConfig) (http.Handler, error) {
	if verifier == nil || service == nil {
		return nil, errors.New("resource settlement endpoint dependencies are invalid")
	}
	return AuthenticateWorkload(verifier, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is required")))
			return
		}
		tenantID, e1 := domain.ParseID(principal.TenantID)
		actorID, e2 := domain.ParseID(principal.ID)
		requestID, e3 := domain.ParseID(requestIDFromContext(r.Context()))
		reservationID, e4 := domain.ParseID(r.PathValue("reservation_id"))
		if e1 != nil || e2 != nil || e3 != nil {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is invalid")))
			return
		}
		if e4 != nil {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeNotFound, "reservation not found")))
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "Idempotency-Key is required")))
			return
		}
		var body settlementBody
		if err := DecodeJSON(r, &body); err != nil {
			writeProblem(w, r, ProblemFromError(err))
			return
		}
		usageIDs := make([]domain.ID, len(body.FinalUsageEventIDs))
		for i, value := range body.FinalUsageEventIDs {
			id, err := domain.ParseID(value)
			if err != nil {
				writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "final usage event identifier is invalid")))
				return
			}
			usageIDs[i] = id
		}
		result, err := service.Settle(r.Context(), application.SettleReservation{TenantID: tenantID, PrincipalID: actorID, RequestID: requestID, ReservationID: reservationID, Roles: principal.Roles, Issuer: principal.Issuer, IdempotencyKey: key, TerminalRunState: body.TerminalRunState, FinalUsageEventIDs: usageIDs, SecurityState: config.SecurityState})
		if err != nil {
			writeProblem(w, r, ProblemFromError(err))
			return
		}
		amounts := func(values []domain.ResourceSettlementAmount) []map[string]any {
			out := make([]map[string]any, 0, len(values))
			for _, v := range values {
				out = append(out, map[string]any{"name": v.Name, "class": "consumable", "unit": v.Unit, "quantity": v.Value})
			}
			return out
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": result.ID.String(), "reservation_id": result.ReservationID.String(), "consumed": amounts(result.Consumed), "returned": amounts(result.Returned), "settled_at": result.SettledAt})
	})), nil
}
