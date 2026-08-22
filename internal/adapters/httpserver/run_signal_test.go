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
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type runSignalServiceStub struct {
	command application.SignalRun
	event   domain.RunEvent
	calls   int
}

func (s *runSignalServiceStub) Signal(_ context.Context, command application.SignalRun) (domain.RunEvent, error) {
	s.command = command
	s.calls++
	return s.event, nil
}

func TestRunSignalHTTPValidatesPersistsAndReplays(t *testing.T) {
	tenant, principal, runID, eventID := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	now := time.Date(2026, 8, 22, 16, 0, 0, 0, time.UTC)
	service := &runSignalServiceStub{event: domain.RunEvent{ID: eventID, RunID: runID, Sequence: 2, Type: "run.signal.accepted", Data: map[string]any{"signal_type": "CUSTOM"}, OccurredAt: now}}
	store := &idempotencyStoreStub{acquisition: ports.IdempotencyAcquisition{Outcome: ports.IdempotencyAcquired, RecordID: mustHTTPID(t), OwnerToken: mustHTTPID(t)}}
	handler, err := RunSignalHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String(), Roles: []string{"agent-invoker"}}}, service, store, fixedHTTPClock{now}, RunSignalHTTPConfig{Lease: time.Minute, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/runs/"+runID.String()+"/signals", strings.NewReader(`{"type":"CUSTOM","payload":{"name":"runtime.refresh","data":{"scope":"tools"}},"expected_state_version":3}`))
	request.SetPathValue("run_id", runID.String())
	request.Header.Set("Authorization", "Bearer valid")
	request.Header.Set("Idempotency-Key", "signal-key-00001")
	request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, mustHTTPID(t).String()))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || service.calls != 1 || service.command.TenantID != tenant || service.command.RunID != runID || service.command.Type != domain.RunSignalCustom || *service.command.ExpectedStateVersion != 3 || store.request.Route != runSignalRoute || store.completed.Status != http.StatusAccepted {
		t.Fatalf("status=%d body=%s command=%+v store=%+v", response.Code, response.Body.String(), service.command, store)
	}
	replayStore := &idempotencyStoreStub{acquisition: ports.IdempotencyAcquisition{Outcome: ports.IdempotencyReplay, Response: &store.completed}}
	replayHandler, _ := RunSignalHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String()}}, service, replayStore, fixedHTTPClock{now}, RunSignalHTTPConfig{Lease: time.Minute, TTL: time.Hour})
	replay := httptest.NewRecorder()
	replayReq := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"type":"CUSTOM","payload":{"name":"runtime.refresh","data":{"scope":"tools"}},"expected_state_version":3}`))
	replayReq.SetPathValue("run_id", runID.String())
	replayReq.Header.Set("Authorization", "Bearer valid")
	replayReq.Header.Set("Idempotency-Key", "signal-key-00001")
	replayReq = replayReq.WithContext(context.WithValue(replayReq.Context(), requestIDKey{}, mustHTTPID(t).String()))
	replayHandler.ServeHTTP(replay, replayReq)
	if replay.Code != http.StatusAccepted || service.calls != 1 || replay.Body.String() != string(store.completed.Body) {
		t.Fatalf("replay=%d %s calls=%d", replay.Code, replay.Body.String(), service.calls)
	}
}

func TestRunSignalHTTPRejectsMalformedTypedPayloadAndOpaqueID(t *testing.T) {
	tenant, principal, runID := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	service := &runSignalServiceStub{}
	store := &idempotencyStoreStub{}
	handler, _ := RunSignalHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String()}}, service, store, fixedHTTPClock{time.Now().UTC()}, RunSignalHTTPConfig{Lease: time.Minute, TTL: time.Hour})
	for _, tc := range []struct {
		name, id, body string
		status         int
	}{{"bad id", "opaque", `{"type":"RESUME","payload":{}}`, http.StatusNotFound}, {"cancel reserved", runID.String(), `{"type":"CANCEL","payload":{}}`, http.StatusBadRequest}, {"bad resume", runID.String(), `{"type":"RESUME","payload":{"extra":true}}`, http.StatusBadRequest}, {"unknown field", runID.String(), `{"type":"RESUME","payload":{},"tenant_id":"attack"}`, http.StatusBadRequest}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			req.SetPathValue("run_id", tc.id)
			req.Header.Set("Authorization", "Bearer valid")
			req.Header.Set("Idempotency-Key", "signal-key-00001")
			req = req.WithContext(context.WithValue(req.Context(), requestIDKey{}, mustHTTPID(t).String()))
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, req)
			if out.Code != tc.status {
				t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
			}
		})
	}
	if service.calls != 0 || store.request.Key != "" {
		t.Fatal("invalid signal reached dependencies")
	}
}
