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

// A stream must stop delivering events when its authorization is revoked.
type revokedRunStream struct{ checks, reads int }

func (s *revokedRunStream) Authorize(context.Context, application.GetRun) error {
	s.checks++
	if s.checks > 1 {
		return domain.NewError(domain.CodeForbidden, "run revoked")
	}
	return nil
}
func (s *revokedRunStream) Events(context.Context, domain.ID, int64, int) ([]domain.RunEvent, error) {
	s.reads++
	return nil, nil
}
func TestRunEventStreamReauthorizesBeforeReadingMoreEvents(t *testing.T) {
	tenant, principal, run := mustHTTPID(t), mustHTTPID(t), mustHTTPID(t)
	codec, _ := domain.NewRunEventCursorCodec([]byte(strings.Repeat("r", 32)))
	service := &revokedRunStream{}
	h, err := RunEventStreamHandler(&fakeVerifier{principal: oidc.Principal{ID: principal.String(), TenantID: tenant.String()}}, service, codec, policySecurityState(), RunEventStreamOptions{HeartbeatInterval: time.Second, PollInterval: time.Millisecond, WriteTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := httptest.NewRequest("GET", "/events", nil).WithContext(context.WithValue(ctx, requestIDKey{}, mustHTTPID(t).String()))
	req.SetPathValue("run_id", run.String())
	req.Header.Set("Authorization", "Bearer valid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if service.checks != 2 || service.reads != 1 || !w.Flushed {
		t.Fatalf("checks=%d reads=%d flushed=%v", service.checks, service.reads, w.Flushed)
	}
}

func TestBothStreamRoutesAvoidUnaryDeadline(t *testing.T) {
	for _, path := range []string{"/v1/runs/019feba6-b9bb-7fff-bfff-ffffffffffff/events", "/v1/trusted/revocations/events"} {
		var logs bytes.Buffer
		deps := testDependencies(t, &logs, false)
		stream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, exists := r.Context().Deadline(); exists {
				t.Error("stream inherited unary deadline")
			}
			w.WriteHeader(204)
		})
		deps.RunEvents = stream
		deps.RevocationDistribution = stream
		w := httptest.NewRecorder()
		newHandler(testConfig(), deps, &Readiness{}).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 204 {
			t.Fatalf("stream status=%d", w.Code)
		}
	}
}

func TestMountedMethodPatternReportsPublishedRouteMetric(t *testing.T) {
	var logs bytes.Buffer
	deps := testDependencies(t, &logs, true)
	deps.RunQuery = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	newHandler(testConfig(), deps, &Readiness{}).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/v1/runs/019feba6-b9bb-7fff-bfff-ffffffffffff", nil))
	w := httptest.NewRecorder()
	deps.Metrics.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(w.Body.String(), `thinkpixelag_http_requests_total{method="GET",route="/v1/runs/{run_id}",status_class="2xx"} 1`) {
		t.Fatal("mounted route was not attributed to its published metric template")
	}
}
