package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type runAdmissionServiceStub struct {
	command application.AdmitRun
	result  domain.RunAdmission
	calls   int
}

func (s *runAdmissionServiceStub) Admit(_ context.Context, command application.AdmitRun) (domain.RunAdmission, error) {
	s.command = command
	s.calls++
	return s.result, nil
}

type idempotencyStoreStub struct {
	acquisition ports.IdempotencyAcquisition
	request     ports.IdempotencyRequest
	tenant      domain.ID
	completed   ports.IdempotencyResponse
	failed      bool
}

func (s *idempotencyStoreStub) AcquireIdempotency(_ context.Context, tenant domain.ID, request ports.IdempotencyRequest, _ time.Time) (ports.IdempotencyAcquisition, error) {
	s.tenant, s.request = tenant, request
	return s.acquisition, nil
}
func (s *idempotencyStoreStub) CompleteIdempotency(_ context.Context, _ domain.ID, _ ports.IdempotencyAcquisition, response ports.IdempotencyResponse, _ time.Time) error {
	s.completed = response
	return nil
}
func (s *idempotencyStoreStub) FailIdempotency(context.Context, domain.ID, ports.IdempotencyAcquisition, time.Time) error {
	s.failed = true
	return nil
}

type fixedHTTPClock struct{ now time.Time }

func (c fixedHTTPClock) Now() time.Time { return c.now }

func TestRunAdmissionHTTPUsesVerifiedAuthorityAndPersistsReplay(t *testing.T) {
	tenant, principal, agent, run, envelope, version, decision := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	now := time.Date(2026, 8, 22, 13, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	service := &runAdmissionServiceStub{result: domain.RunAdmission{RunID: run, EnvelopeID: envelope, TenantID: tenant, AgentID: agent, AgentVersionID: version, AgentVersionDigest: digest, RequestedBy: principal, PolicyDecisionID: decision, State: domain.RunAdmitted, StateVersion: 1, Constraints: map[string]any{"max_llm_tokens": float64(10)}, CreatedAt: now, UpdatedAt: now}}
	store := &idempotencyStoreStub{acquisition: ports.IdempotencyAcquisition{Outcome: ports.IdempotencyAcquired, RecordID: mustHTTPID(t), OwnerToken: mustHTTPID(t)}}
	verifier := &fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String(), Roles: []string{"invoker"}}}
	handler, err := RunAdmissionHandler(verifier, service, store, fixedHTTPClock{now}, RunAdmissionHTTPConfig{AuthorityConstraints: map[string]any{"max_llm_tokens": float64(100)}, Lease: time.Minute, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	deps := testDependencies(t, &logs, false)
	deps.RunAdmission = handler
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/agents/"+agent.String()+"/runs", strings.NewReader(`{"objective":"restricted payload","input":{"tenant_id":"attacker","secret":"not evidence"},"constraints":{"max_llm_tokens":10}}`))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("Idempotency-Key", "request-key-0001")
	newHandler(testConfig(), deps, &Readiness{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || service.calls != 1 || service.command.TenantID != tenant || service.command.PrincipalID != principal || service.command.AgentID != agent || store.tenant != tenant {
		t.Fatalf("response=%d %s command=%+v", recorder.Code, recorder.Body.String(), service.command)
	}
	if service.command.RequestedConstraints["max_llm_tokens"].(json.Number).String() != "10" || store.request.Route != runAdmissionRoute || store.request.RequestHash == "" || store.completed.Status != http.StatusCreated {
		t.Fatalf("idempotency=%+v completed=%+v", store.request, store.completed)
	}
	if strings.Contains(string(store.completed.Body), "restricted") || recorder.Header().Get("Location") != "/v1/runs/"+run.String() {
		t.Fatalf("restricted payload leaked or Location absent: %s headers=%v", recorder.Body.String(), recorder.Header())
	}

	replay := &idempotencyStoreStub{acquisition: ports.IdempotencyAcquisition{Outcome: ports.IdempotencyReplay, Response: &store.completed}}
	replayHandler, _ := RunAdmissionHandler(verifier, service, replay, fixedHTTPClock{now}, RunAdmissionHTTPConfig{AuthorityConstraints: map[string]any{}, Lease: time.Minute, TTL: time.Hour})
	response := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, "/v1/agents/"+agent.String()+"/runs", strings.NewReader(`{"objective":"restricted payload","input":{"tenant_id":"attacker","secret":"not evidence"},"constraints":{"max_llm_tokens":10}}`))
	replayRequest.SetPathValue("agent_id", agent.String())
	replayRequest.Header.Set("Authorization", "Bearer valid")
	replayRequest.Header.Set("Idempotency-Key", "request-key-0001")
	requestID := mustHTTPID(t).String()
	replayRequest = replayRequest.WithContext(context.WithValue(replayRequest.Context(), requestIDKey{}, requestID))
	replayHandler.ServeHTTP(response, replayRequest)
	if response.Code != http.StatusCreated || service.calls != 1 || response.Body.String() != string(store.completed.Body) {
		t.Fatalf("replay=%d %s calls=%d", response.Code, response.Body.String(), service.calls)
	}
}

func TestRunAdmissionHTTPRejectsBoundaryViolationsBeforeAdmission(t *testing.T) {
	tenant, principal, agent := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	service := &runAdmissionServiceStub{}
	store := &idempotencyStoreStub{}
	handler, _ := RunAdmissionHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String()}}, service, store, fixedHTTPClock{time.Now().UTC()}, RunAdmissionHTTPConfig{AuthorityConstraints: map[string]any{}, Lease: time.Minute, TTL: time.Hour})
	tests := []struct{ name, key, body string }{
		{"missing key", "", `{"objective":"x"}`},
		{"empty objective", "request-key-0001", `{"objective":""}`},
		{"unknown authority field", "request-key-0001", `{"objective":"x","tenant_id":"other"}`},
		{"invalid explicit digest", "request-key-0001", `{"objective":"x","requested_version_digest":"latest"}`},
		{"negative constraint", "request-key-0001", `{"objective":"x","constraints":{"max_tool_calls":-1}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/agents/"+agent.String()+"/runs", strings.NewReader(test.body))
			request.SetPathValue("agent_id", agent.String())
			request.Header.Set("Authorization", "Bearer valid")
			if test.key != "" {
				request.Header.Set("Idempotency-Key", test.key)
			}
			request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, mustHTTPID(t).String()))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if service.calls != 0 || store.request.Key != "" {
		t.Fatalf("invalid request reached dependencies")
	}
}
