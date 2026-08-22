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
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type runCancellationServiceStub struct {
	command application.CancelRun
	run     domain.Run
	calls   int
}

func (stub *runCancellationServiceStub) Cancel(_ context.Context, command application.CancelRun) (domain.Run, error) {
	stub.command = command
	stub.calls++
	return stub.run, nil
}

func TestRunCancellationHTTPUsesAuthorityAndReplays(t *testing.T) {
	tenant, principal, runID, agent, version := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	now := time.Date(2026, 8, 22, 18, 0, 0, 0, time.UTC)
	service := &runCancellationServiceStub{run: domain.Run{ID: runID, TenantID: tenant, AgentID: agent, AgentVersionID: version, RequestedBy: principal, VersionDigest: "sha256:" + strings.Repeat("c", 64), State: domain.RunCancelled, StateVersion: 4, EnvelopeVersion: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now}}
	store := &idempotencyStoreStub{acquisition: ports.IdempotencyAcquisition{Outcome: ports.IdempotencyAcquired, RecordID: mustHTTPID(t), OwnerToken: mustHTTPID(t)}}
	handler, err := RunCancellationHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String(), Roles: []string{"agent-invoker"}}}, service, store, fixedHTTPClock{now}, RunCancellationHTTPConfig{Lease: time.Minute, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/runs/"+runID.String()+"/cancel", strings.NewReader(`{"reason_code":"caller.request","expected_state_version":3}`))
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("Idempotency-Key", "cancel-key-00001")
	response := httptest.NewRecorder()
	var logs bytes.Buffer
	dependencies := testDependencies(t, &logs, false)
	dependencies.RunCancellation = handler
	newHandler(testConfig(), dependencies, &Readiness{}).ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.calls != 1 || service.command.TenantID != tenant || service.command.RunID != runID || service.command.ReasonCode != "caller.request" || store.request.Route != runCancelRoute || store.completed.Status != http.StatusOK {
		t.Fatalf("status=%d body=%s command=%+v store=%+v", response.Code, response.Body.String(), service.command, store)
	}
	replayStore := &idempotencyStoreStub{acquisition: ports.IdempotencyAcquisition{Outcome: ports.IdempotencyReplay, Response: &store.completed}}
	replayHandler, _ := RunCancellationHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String()}}, service, replayStore, fixedHTTPClock{now}, RunCancellationHTTPConfig{Lease: time.Minute, TTL: time.Hour})
	replay := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"reason_code":"caller.request","expected_state_version":3}`))
	replayRequest.SetPathValue("run_id", runID.String())
	replayRequest.Header.Set("Authorization", "Bearer valid")
	replayRequest.Header.Set("Idempotency-Key", "cancel-key-00001")
	replayRequest = replayRequest.WithContext(context.WithValue(replayRequest.Context(), requestIDKey{}, mustHTTPID(t).String()))
	replayHandler.ServeHTTP(replay, replayRequest)
	if replay.Code != http.StatusOK || service.calls != 1 || replay.Body.String() != string(store.completed.Body) {
		t.Fatalf("replay=%d %s calls=%d", replay.Code, replay.Body.String(), service.calls)
	}
}

func TestRunCancellationHTTPRejectsMalformedInput(t *testing.T) {
	tenant, principal, runID := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	service := &runCancellationServiceStub{}
	handler, _ := RunCancellationHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String()}}, service, &idempotencyStoreStub{}, fixedHTTPClock{time.Now().UTC()}, RunCancellationHTTPConfig{Lease: time.Minute, TTL: time.Hour})
	for _, test := range []struct{ id, body string }{{"opaque", `{}`}, {runID.String(), `{"reason_code":"bad reason"}`}, {runID.String(), `{"expected_state_version":0}`}, {runID.String(), `{"tenant_id":"attack"}`}} {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
		request.SetPathValue("run_id", test.id)
		request.Header.Set("Authorization", "Bearer valid")
		request.Header.Set("Idempotency-Key", "cancel-key-00001")
		request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, mustHTTPID(t).String()))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if test.id == "opaque" && response.Code != http.StatusNotFound || test.id != "opaque" && response.Code != http.StatusBadRequest {
			t.Fatalf("id=%s body=%s status=%d response=%s", test.id, test.body, response.Code, response.Body.String())
		}
	}
	if service.calls != 0 {
		t.Fatal("invalid request reached cancellation service")
	}
}
