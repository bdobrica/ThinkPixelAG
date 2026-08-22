package httpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

type runQueryServiceStub struct {
	command application.GetRun
	result  domain.Run
	err     error
	calls   int
}

func (s *runQueryServiceStub) Get(_ context.Context, command application.GetRun) (domain.Run, error) {
	s.command = command
	s.calls++
	return s.result, s.err
}

func TestRunQueryHTTPUsesVerifiedAuthorityAndReturnsContract(t *testing.T) {
	tenant, principal, runID, agent, version := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	now := time.Date(2026, 8, 22, 14, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("c", 64)
	service := &runQueryServiceStub{result: domain.Run{ID: runID, TenantID: tenant, AgentID: agent, AgentVersionID: version, RequestedBy: principal, VersionDigest: digest, State: domain.RunAdmitted, StateVersion: 1, EnvelopeVersion: 1, CreatedAt: now, UpdatedAt: now}}
	handler, _ := RunQueryHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String(), Roles: []string{"agent-invoker"}}}, service, policySecurityState())
	var logs bytes.Buffer
	deps := testDependencies(t, &logs, false)
	deps.RunQuery = handler
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/runs/"+runID.String(), nil)
	request.Header.Set("Authorization", "Bearer valid")
	newHandler(testConfig(), deps, &Readiness{}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.calls != 1 || service.command.TenantID != tenant || service.command.PrincipalID != principal || service.command.RunID != runID || !strings.Contains(response.Body.String(), digest) {
		t.Fatalf("status=%d body=%s command=%+v", response.Code, response.Body.String(), service.command)
	}
}

func TestRunQueryHTTPInvalidOpaqueIDIsNotFound(t *testing.T) {
	tenant, principal := mustHTTPID(t), mustHTTPID(t)
	service := &runQueryServiceStub{}
	handler, _ := RunQueryHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String()}}, service, policySecurityState())
	request := httptest.NewRequest(http.MethodGet, "/v1/runs/not-a-uuid", nil)
	request.SetPathValue("run_id", "not-a-uuid")
	request.Header.Set("Authorization", "Bearer valid")
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, mustHTTPID(t).String()))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || service.calls != 0 || !strings.Contains(response.Body.String(), `"detail":"run not found"`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func policySecurityState() policy.SecurityState { return policy.SecurityState{} }
