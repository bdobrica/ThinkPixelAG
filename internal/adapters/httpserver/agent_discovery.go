package httpserver

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

const agentCursorSortKey = "agent_id"

type AgentDiscoveryService interface {
	List(context.Context, application.DiscoverAgents) (application.AgentPage, error)
	Describe(context.Context, application.DiscoverAgents, domain.ID) (domain.Agent, error)
}

// AgentDiscoveryHandler constructs authenticated public list and describe
// endpoints. Tenant and principal authority come exclusively from verified
// identity; cursors are authenticated continuation state, never authority.
func AgentDiscoveryHandler(verifier oidc.Verifier, service AgentDiscoveryService, cursors *domain.CursorCodec) (http.Handler, error) {
	if verifier == nil || service == nil || cursors == nil {
		return nil, domain.NewError(domain.CodeInternal, "agent discovery endpoint dependencies are unavailable")
	}
	return AuthenticateBearer(verifier, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			writeProblem(writer, request, problemFor(http.StatusMethodNotAllowed, "method_not_allowed", "Method Not Allowed", "The request method is not supported for this resource."))
			return
		}
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
		command := application.DiscoverAgents{TenantID: tenantID, PrincipalID: principalID, RequestID: requestID, Roles: principal.Roles, Issuer: principal.Issuer, Limit: 50}
		if raw := request.URL.Query().Get("page_size"); raw != "" {
			limit, err := strconv.Atoi(raw)
			if err != nil || limit < 1 || limit > 200 {
				writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "page_size must be an integer from 1 to 200")))
				return
			}
			command.Limit = limit
		}
		if raw := request.URL.Query().Get("cursor"); raw != "" {
			cursor, err := cursors.Decode(raw)
			if err != nil || cursor.SortKey != agentCursorSortKey {
				writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInvalidArgument, "pagination cursor is invalid")))
				return
			}
			command.After = cursor.ID
		}
		if strings.TrimPrefix(request.URL.Path, "/v1/agents") == "" {
			page, err := service.List(request.Context(), command)
			if err != nil {
				writeProblem(writer, request, ProblemFromError(err))
				return
			}
			response := map[string]any{"items": publicAgents(page.Items)}
			if !page.Next.IsZero() {
				next, err := cursors.Encode(domain.PageCursor{SortKey: agentCursorSortKey, ID: page.Next})
				if err != nil {
					writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeInternal, "could not encode pagination cursor")))
					return
				}
				response["next_cursor"] = next
			}
			writeJSON(writer, http.StatusOK, response)
			return
		}
		agentID, err := domain.ParseID(request.PathValue("agent_id"))
		if err != nil {
			// Invalid opaque identifiers are indistinguishable from absent ones on
			// detail routes, preventing an identifier-format enumeration oracle.
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeNotFound, "agent not found")))
			return
		}
		agent, err := service.Describe(request.Context(), command, agentID)
		if err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		writeJSON(writer, http.StatusOK, publicAgent(agent))
	})), nil
}

func publicAgents(agents []domain.Agent) []map[string]any {
	items := make([]map[string]any, len(agents))
	for i, agent := range agents {
		items[i] = publicAgent(agent)
	}
	return items
}

func publicAgent(agent domain.Agent) map[string]any {
	return map[string]any{"id": agent.ID.String(), "name": agent.Name, "description": agent.Description, "owner": agent.OwnerPrincipalID.String(), "sponsor": agent.SponsorPrincipalID.String(), "risk_class": strings.ToLower(string(agent.RiskClass)), "state": string(agent.Status), "created_at": agent.CreatedAt}
}
