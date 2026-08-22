package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type approvalServiceStub struct {
	command application.DecideAgentVersion
}

func (stub *approvalServiceStub) Decide(_ context.Context, command application.DecideAgentVersion) (domain.AgentVersionApproval, error) {
	stub.command = command
	return domain.AgentVersionApproval{ID: command.ID, ActorPrincipalID: command.ActorPrincipalID, Decision: command.Decision, CreatedAt: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}, nil
}

func TestAgentApprovalHandlerAuthenticatesDecodesAndDelegatesAuthorization(t *testing.T) {
	t.Parallel()
	actor, _ := domain.NewID()
	tenant, _ := domain.NewID()
	agent, _ := domain.NewID()
	approval, _ := domain.NewID()
	verifier := &fakeVerifier{principal: oidc.Principal{ID: actor.String(), TenantID: tenant.String(), Roles: []string{"registry-admin"}}}
	service := &approvalServiceStub{}
	handler, err := AgentApprovalHandler(verifier, service, func() (string, error) { return approval.String(), nil })
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/admin/agents/{agent_id}/versions/{version_digest}/approvals", handler)
	wrapped := requestID(mux, func() (string, error) { return fixedRequestID, nil })
	digest := "sha256:" + strings.Repeat("a", 64)
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/agents/"+agent.String()+"/versions/"+digest+"/approvals", strings.NewReader(`{"decision":"revoke","reason_code":"security.revoked","approval_reference":"incident-7"}`))
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	wrapped.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.command.Decision != domain.DecisionRevoke || service.command.TenantID != tenant || service.command.AgentID != agent || service.command.RequestID.String() != fixedRequestID {
		t.Fatalf("status=%d command=%+v body=%s", response.Code, service.command, response.Body.String())
	}
}
