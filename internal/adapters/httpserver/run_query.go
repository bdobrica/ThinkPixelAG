package httpserver

import (
	"context"
	"net/http"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

type RunQueryService interface {
	Get(context.Context, application.GetRun) (domain.Run, error)
}

func RunQueryHandler(verifier oidc.Verifier, service RunQueryService, securityState policy.SecurityState) (http.Handler, error) {
	if verifier == nil || service == nil {
		return nil, domain.NewError(domain.CodeInternal, "run query endpoint dependencies are unavailable")
	}
	return AuthenticateBearer(verifier, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		principal, ok := PrincipalFromContext(request.Context())
		if !ok {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is required")))
			return
		}
		tenantID, tenantErr := domain.ParseID(principal.TenantID)
		principalID, principalErr := domain.ParseID(principal.ID)
		requestID, requestErr := domain.ParseID(requestIDFromContext(request.Context()))
		if tenantErr != nil || principalErr != nil || requestErr != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is invalid")))
			return
		}
		runID, err := domain.ParseID(request.PathValue("run_id"))
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeNotFound, "run not found")))
			return
		}
		run, err := service.Get(request.Context(), application.GetRun{TenantID: tenantID, PrincipalID: principalID, RequestID: requestID, RunID: runID, Roles: principal.Roles, Issuer: principal.Issuer, SecurityState: securityState})
		if err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		writeJSON(writer, http.StatusOK, publicRun(run))
	})), nil
}

func publicRun(run domain.Run) map[string]any {
	envelope := map[string]any{"version": run.EnvelopeVersion, "resources": []any{}}
	if run.DeadlineAt != nil {
		envelope["run_deadline"] = run.DeadlineAt
	}
	response := map[string]any{"id": run.ID.String(), "agent_id": run.AgentID.String(), "version_digest": run.VersionDigest, "state": string(run.State), "state_version": run.StateVersion, "envelope": envelope, "created_at": run.CreatedAt, "updated_at": run.UpdatedAt}
	if run.ParentRunID != nil {
		response["parent_run_id"] = run.ParentRunID.String()
	}
	return response
}
