package httpserver

import (
	"context"
	"net/http"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

type ResourceExtensionApplication interface {
	Extend(context.Context, application.ExtendResources) (domain.ResourceExtensionResult, error)
}
type ResourceExtensionHTTPConfig struct{ SecurityState policy.SecurityState }

func ResourceExtensionHandler(verifier oidc.Verifier, service ResourceExtensionApplication, config ResourceExtensionHTTPConfig) (http.Handler, error) {
	if verifier == nil || service == nil {
		return nil, domain.NewError(domain.CodeInternal, "resource extension endpoint dependencies are unavailable")
	}
	return AuthenticateBearer(verifier, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is required")))
			return
		}
		actor, err := domain.ParseID(principal.ID)
		if err != nil {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "authenticated principal identifier is invalid")))
			return
		}
		tenant, err := domain.ParseID(principal.TenantID)
		if err != nil {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "authenticated tenant identifier is invalid")))
			return
		}
		run, err := domain.ParseID(r.PathValue("run_id"))
		if err != nil {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "run identifier is invalid")))
			return
		}
		requestID, err := domain.ParseID(requestIDFromContext(r.Context()))
		if err != nil {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInternal, "request identifier is invalid")))
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "Idempotency-Key is required")))
			return
		}
		var body struct {
			Additions []struct {
				Name, Class, Unit string
				Quantity          int64
			} `json:"additions"`
			DeadlineExtensionSeconds int64  `json:"deadline_extension_seconds"`
			ReasonCode               string `json:"reason_code"`
			ApprovalReference        string `json:"approval_reference"`
		}
		if err := DecodeJSON(r, &body); err != nil {
			writeProblem(w, r, ProblemFromError(err))
			return
		}
		additions := make([]domain.ResourceExtensionAmount, 0, len(body.Additions))
		for _, item := range body.Additions {
			if item.Class != "consumable" || item.Quantity <= 0 {
				writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "extension additions must be positive consumables")))
				return
			}
			decimal, err := domain.NewDecimal(item.Quantity, 0)
			if err != nil {
				writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "extension quantity is invalid")))
				return
			}
			quantity, err := domain.NewQuantity(decimal, item.Unit)
			if err != nil {
				writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "extension quantity is invalid")))
				return
			}
			additions = append(additions, domain.ResourceExtensionAmount{Name: item.Name, Quantity: quantity})
		}
		result, err := service.Extend(r.Context(), application.ExtendResources{TenantID: tenant, PrincipalID: actor, RequestID: requestID, RunID: run, Roles: principal.Roles, Issuer: principal.Issuer, IdempotencyKey: key, ReasonCode: body.ReasonCode, ApprovalReference: body.ApprovalReference, Additions: additions, DeadlineExtensionSeconds: body.DeadlineExtensionSeconds, SecurityState: config.SecurityState})
		if err != nil {
			writeProblem(w, r, ProblemFromError(err))
			return
		}
		response := map[string]any{"version": result.EnvelopeVersion, "resources": []any{}}
		if result.DeadlineAt != nil {
			response["run_deadline"] = result.DeadlineAt
		}
		writeJSON(w, http.StatusCreated, response)
	})), nil
}
