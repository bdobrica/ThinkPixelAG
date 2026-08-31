package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type extensionHTTPStub struct {
	command application.ExtendResources
	calls   int
}

func (s *extensionHTTPStub) Extend(_ context.Context, command application.ExtendResources) (domain.ResourceExtensionResult, error) {
	s.calls++
	s.command = command
	id, _ := domain.NewID()
	return domain.ResourceExtensionResult{ID: id, EnvelopeVersion: 2}, nil
}

func TestResourceExtensionBindsPrivilegedActorAndApproval(t *testing.T) {
	tenant, actor, run := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	service := &extensionHTTPStub{}
	handler, _ := ResourceExtensionHandler(&fakeVerifier{principal: oidc.Principal{ID: actor.String(), TenantID: tenant.String(), Issuer: "https://issuer.example", Roles: []string{"resource-admin"}}}, service, ResourceExtensionHTTPConfig{})
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/runs/"+run.String()+"/resource-extensions", strings.NewReader(`{"additions":[{"name":"llm_tokens","class":"consumable","unit":"llm_tokens","quantity":25}],"deadline_extension_seconds":30,"reason_code":"budget.increase","approval_reference":"CAB-42"}`))
	request.SetPathValue("run_id", run.String())
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("Idempotency-Key", "extend-1")
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, mustHTTPID(t).String()))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.calls != 1 || service.command.PrincipalID != actor || service.command.ApprovalReference != "CAB-42" || service.command.DeadlineExtensionSeconds != 30 || len(service.command.Additions) != 1 {
		t.Fatalf("status=%d calls=%d command=%+v body=%s", response.Code, service.calls, service.command, response.Body.String())
	}
}
