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
)

type runEventStreamStub struct {
	command application.GetRun
	events  []domain.RunEvent
	err     error
	after   int64
	cancel  context.CancelFunc
}

func (s *runEventStreamStub) Authorize(_ context.Context, command application.GetRun) error {
	s.command = command
	return nil
}
func (s *runEventStreamStub) Events(_ context.Context, _ domain.ID, after int64, _ int) ([]domain.RunEvent, error) {
	s.after = after
	if s.cancel != nil {
		go func() { time.Sleep(5 * time.Millisecond); s.cancel() }()
	}
	result := s.events
	s.events = nil
	return result, s.err
}

func TestRunEventSSEEmitsCursorAndResumesWithLastEventID(t *testing.T) {
	tenant, principal, runID, eventID := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	codec, _ := domain.NewRunEventCursorCodec([]byte(strings.Repeat("c", 32)))
	ctx, cancel := context.WithCancel(context.Background())
	service := &runEventStreamStub{events: []domain.RunEvent{{ID: eventID, RunID: runID, Sequence: 7, Type: "run.signal.accepted", Data: map[string]any{"safe": true}, OccurredAt: time.Now().UTC()}}, cancel: cancel}
	handler, _ := RunEventStreamHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String()}}, service, codec, policySecurityState(), RunEventStreamOptions{HeartbeatInterval: time.Second, PollInterval: time.Millisecond, WriteTimeout: time.Second})
	request := httptest.NewRequest(http.MethodGet, "/v1/runs/"+runID.String()+"/events", nil).WithContext(context.WithValue(ctx, requestIDKey{}, mustHTTPID(t).String()))
	request.SetPathValue("run_id", runID.String())
	request.Header.Set("Authorization", "Bearer valid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, "event: run.signal.accepted") || !strings.Contains(body, `"sequence":7`) || service.command.TenantID != tenant {
		t.Fatalf("status=%d body=%s", response.Code, body)
	}
	idLine := strings.Split(strings.Split(body, "id: ")[1], "\n")[0]
	if sequence, err := codec.Decode(idLine, runID); err != nil || sequence != 7 {
		t.Fatalf("cursor sequence=%d err=%v", sequence, err)
	}
}

func TestRunEventSSERejectsInvalidAndExpiredCursorsBeforeStreaming(t *testing.T) {
	tenant, principal, runID := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	codec, _ := domain.NewRunEventCursorCodec([]byte(strings.Repeat("d", 32)))
	for _, tc := range []struct {
		cursor string
		err    error
		status int
	}{{"invalid", nil, http.StatusBadRequest}, {"", domain.ErrRunEventCursorGone, http.StatusGone}} {
		service := &runEventStreamStub{err: tc.err}
		handler, _ := RunEventStreamHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String()}}, service, codec, policySecurityState(), RunEventStreamOptions{HeartbeatInterval: time.Second, PollInterval: time.Second, WriteTimeout: time.Second})
		request := httptest.NewRequest(http.MethodGet, "/?after="+tc.cursor, nil)
		request.SetPathValue("run_id", runID.String())
		request.Header.Set("Authorization", "Bearer valid")
		request = request.WithContext(context.WithValue(request.Context(), requestIDKey{}, mustHTTPID(t).String()))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status || strings.HasPrefix(response.Header().Get("Content-Type"), "text/event-stream") {
			t.Fatalf("cursor=%q status=%d body=%s", tc.cursor, response.Code, response.Body.String())
		}
	}
}

func TestRunEventRouteIsMounted(t *testing.T) {
	var logs bytes.Buffer
	deps := testDependencies(t, &logs, false)
	deps.RunEvents = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	response := httptest.NewRecorder()
	newHandler(testConfig(), deps, &Readiness{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/runs/019feba6-b9bb-7fff-bfff-ffffffffffff/events", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
