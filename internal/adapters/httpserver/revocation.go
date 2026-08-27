package httpserver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

type RevocationApplication interface {
	Create(context.Context, application.ChangeRevocation) (domain.RevocationResult, error)
	Lift(context.Context, application.ChangeRevocation) (domain.RevocationResult, error)
}
type RevocationHTTPConfig struct{ SecurityState policy.SecurityState }

func RevocationHandler(verifier oidc.Verifier, service RevocationApplication, config RevocationHTTPConfig) (http.Handler, error) {
	if verifier == nil || service == nil {
		return nil, domain.NewError(domain.CodeInternal, "revocation endpoint dependencies are unavailable")
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
		requestID, err := domain.ParseID(requestIDFromContext(r.Context()))
		if err != nil {
			writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInternal, "request identifier is invalid")))
			return
		}
		command := application.ChangeRevocation{TenantID: tenant, PrincipalID: actor, RequestID: requestID, Roles: principal.Roles, Issuer: principal.Issuer, SecurityState: config.SecurityState}
		if raw := r.PathValue("revocation_id"); raw != "" {
			command.RevocationID, err = domain.ParseID(raw)
			if err != nil {
				writeProblem(w, r, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "revocation identifier is invalid")))
				return
			}
			var body struct {
				ReasonCode        string `json:"reason_code"`
				ApprovalReference string `json:"approval_reference"`
			}
			if err = DecodeJSON(r, &body); err != nil {
				writeProblem(w, r, ProblemFromError(err))
				return
			}
			command.ReasonCode = body.ReasonCode
			command.ApprovalReference = body.ApprovalReference
			result, e := service.Lift(r.Context(), command)
			if e != nil {
				writeProblem(w, r, ProblemFromError(e))
				return
			}
			writeJSON(w, http.StatusOK, revocationResponse(result))
			return
		}
		var body struct {
			Scope             string     `json:"scope"`
			Target            string     `json:"target"`
			ReasonCode        string     `json:"reason_code"`
			DetailReference   string     `json:"detail_reference"`
			EffectiveAt       time.Time  `json:"effective_at"`
			ExpiresAt         *time.Time `json:"expires_at"`
			ApprovalReference string     `json:"approval_reference"`
		}
		if err = DecodeJSON(r, &body); err != nil {
			writeProblem(w, r, ProblemFromError(err))
			return
		}
		command.Scope = domain.RevocationScope(strings.ToUpper(body.Scope))
		command.Target = body.Target
		command.ReasonCode = body.ReasonCode
		command.DetailReference = body.DetailReference
		command.EffectiveAt = body.EffectiveAt
		command.ExpiresAt = body.ExpiresAt
		command.ApprovalReference = body.ApprovalReference
		result, e := service.Create(r.Context(), command)
		if e != nil {
			writeProblem(w, r, ProblemFromError(e))
			return
		}
		writeJSON(w, http.StatusCreated, revocationResponse(result))
	})), nil
}
func revocationResponse(r domain.RevocationResult) map[string]any {
	state := "ACTIVE"
	if r.State == domain.RevocationLifted {
		state = "LIFTED"
	}
	v := r.Revocation
	response := map[string]any{"id": r.RevocationID.String(), "scope": strings.ToLower(string(v.Scope)), "target": v.Target, "state": state, "reason_code": v.ReasonCode, "effective_at": v.EffectiveAt, "created_at": v.CreatedAt, "epochs": map[string]int64{"security_epoch": r.Epochs.Security, "tenant_policy_epoch": r.Epochs.TenantPolicy, "tenant_revocation_epoch": r.Epochs.TenantRevocation, "agent_revocation_epoch": r.Epochs.AgentRevocation}}
	if v.ExpiresAt != nil {
		response["expires_at"] = v.ExpiresAt
	}
	return response
}
