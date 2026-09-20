package httpserver

import (
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"net/http"
)

func administrationCaller(r *http.Request) (application.AdminCaller, error) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		return application.AdminCaller{}, domain.NewError(domain.CodeUnauthenticated, "verified caller required")
	}
	tenant, e1 := domain.ParseID(p.TenantID)
	actor, e2 := domain.ParseID(p.ID)
	request, e3 := domain.ParseID(requestIDFromContext(r.Context()))
	if e1 != nil || e2 != nil || e3 != nil {
		return application.AdminCaller{}, domain.NewError(domain.CodeUnauthenticated, "valid caller identifiers required")
	}
	return application.AdminCaller{TenantID: tenant, PrincipalID: actor, RequestID: request, Roles: p.Roles, Key: r.Header.Get("Idempotency-Key")}, nil
}
func PolicyAdministrationHandler(verifier oidc.Verifier, service *application.PolicyAdministration) http.Handler {
	return AuthenticateBearer(verifier, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, err := administrationCaller(r)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		var result any
		if len(r.Header.Values("Idempotency-Key")) != 1 {
			WriteError(w, r, domain.NewError(domain.CodeInvalidArgument, "one Idempotency-Key required"))
			return
		}
		if digest := r.PathValue("policy_digest"); digest != "" {
			var b application.PolicyActivate
			if err = DecodeJSON(r, &b); err == nil {
				result, err = service.Activate(r.Context(), caller, digest, b)
			}
		} else {
			var b application.PolicyUpload
			if err = DecodeJSON(r, &b); err == nil {
				result, err = service.Upload(r.Context(), caller, b)
			}
		}
		if err != nil {
			WriteError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}))
}
