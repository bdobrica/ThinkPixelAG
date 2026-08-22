package httpserver

import (
	"context"
	"net/http"
	"strings"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type AgentApprovalService interface {
	Decide(context.Context, application.DecideAgentVersion) (domain.AgentVersionApproval, error)
}

// AgentApprovalHandler constructs the authenticated administrative endpoint.
// Authorization remains mandatory in the application service and therefore
// cannot be bypassed by invoking it outside HTTP.
func AgentApprovalHandler(verifier oidc.Verifier, service AgentApprovalService, newID IDGenerator) (http.Handler, error) {
	if verifier == nil || service == nil || newID == nil {
		return nil, domain.NewError(domain.CodeInternal, "agent approval endpoint dependencies are unavailable")
	}
	return AuthenticateBearer(verifier, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			writeProblem(writer, request, problemFor(http.StatusMethodNotAllowed, "method_not_allowed", "Method Not Allowed", "The request method is not supported for this resource."))
			return
		}
		principal, ok := PrincipalFromContext(request.Context())
		if !ok {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "verified identity is required")))
			return
		}
		actorID, err := domain.ParseID(principal.ID)
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "authenticated principal identifier is invalid")))
			return
		}
		tenantID, err := domain.ParseID(principal.TenantID)
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "authenticated tenant identifier is invalid")))
			return
		}
		agentID, err := domain.ParseID(request.PathValue("agent_id"))
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "agent identifier is invalid")))
			return
		}
		var body struct {
			Decision          string `json:"decision"`
			ReasonCode        string `json:"reason_code"`
			ApprovalReference string `json:"approval_reference"`
		}
		if err := DecodeJSON(request, &body); err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		decisions := map[string]domain.AgentVersionDecision{"approve": domain.DecisionApprove, "reject": domain.DecisionReject, "deprecate": domain.DecisionDeprecate, "revoke": domain.DecisionRevoke}
		decision, ok := decisions[strings.ToLower(body.Decision)]
		if !ok {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "approval decision is invalid")))
			return
		}
		approvalIDText, err := newID()
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "could not generate approval identifier")))
			return
		}
		approvalID, err := domain.ParseID(approvalIDText)
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "generated approval identifier is invalid")))
			return
		}
		requestID, err := domain.ParseID(requestIDFromContext(request.Context()))
		if err != nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "request identifier is invalid")))
			return
		}
		approval, err := service.Decide(request.Context(), application.DecideAgentVersion{ID: approvalID, TenantID: tenantID, AgentID: agentID, ActorPrincipalID: actorID, RequestID: requestID,
			VersionDigest: request.PathValue("version_digest"), Decision: decision, ReasonCode: body.ReasonCode, ApprovalReference: body.ApprovalReference, Roles: principal.Roles})
		if err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		writeJSON(writer, http.StatusCreated, map[string]any{"id": approval.ID.String(), "decision": strings.ToLower(string(approval.Decision)), "actor_id": approval.ActorPrincipalID.String(), "created_at": approval.CreatedAt})
	})), nil
}
