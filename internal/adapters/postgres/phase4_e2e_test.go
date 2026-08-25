//go:build e2e

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/httpserver"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/logging"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/metrics"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/tracing"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPhase4RunLifecycleWorkflow proves that the OpenAPI lifecycle contract's
// mounted HTTP boundaries preserve identity, idempotency, ordering, and state
// machine guarantees when composed with the real PostgreSQL repositories.
func TestPhase4RunLifecycleWorkflow(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("THINKPIXELAG_TEST_DATABASE_URL is required for the Phase 4 end-to-end suite")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := NewMigrator(ctx, connection, os.DirFS(projectMigrationsDir(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if err := connection.Close(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := fixedE2EClock{now: now}
	tenantID, foreignTenantID := newE2EID(t), newE2EID(t)
	principalID, foreignPrincipalID, sponsorID := newE2EID(t), newE2EID(t), newE2EID(t)
	for _, tenant := range []domain.ID{tenantID, foreignTenantID} {
		if _, err := pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, tenant.String(), "run010-"+tenant.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	for _, principal := range []struct{ id, tenant domain.ID }{{principalID, tenantID}, {sponsorID, tenantID}, {foreignPrincipalID, foreignTenantID}} {
		if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES($1,$2,'https://run010.test',$3,'HUMAN',$4)`, principal.id.String(), principal.tenant.String(), principal.id.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE tenant_id IN ($1,$2)`, tenantID.String(), foreignTenantID.String())
	})

	repositories, err := NewRepositories(pool)
	if err != nil {
		t.Fatal(err)
	}
	repository, _ := repositories.ForTenant(tenantID)
	foreignRepository, _ := repositories.ForTenant(foreignTenantID)
	agentID, versionID, approvalID := newE2EID(t), newE2EID(t), newE2EID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO agents(id,tenant_id,name,owner_principal_id,sponsor_principal_id,risk_class,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'MEDIUM',$6,$6)`, agentID.String(), tenantID.String(), "phase4-"+agentID.String(), principalID.String(), sponsorID.String(), now); err != nil {
		t.Fatal(err)
	}
	manifest, err := domain.NewAgentManifest("registry.example/phase4@sha256:"+strings.Repeat("a", 64), nil, nil, nil, nil, domain.AgentLimits{})
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := manifest.ContentDigest()
	version := domain.AgentVersion{ID: versionID, TenantID: tenantID, AgentID: agentID, ContentDigest: digest, ImageDigest: "sha256:" + strings.Repeat("a", 64), Manifest: manifest, CreatedBy: principalID, CreatedAt: now}
	if err := repository.RegisterAgentVersion(ctx, version, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agent_version_approvals(id,tenant_id,agent_id,agent_version_id,decision,actor_principal_id,policy_decision_id,reason_code,created_at) VALUES($1,$2,$3,$4,'APPROVED',$5,$6,'registry.version.approved',$7)`, approvalID.String(), tenantID.String(), agentID.String(), versionID.String(), principalID.String(), newE2EID(t).String(), now); err != nil {
		t.Fatal(err)
	}

	evaluator := phase4PolicyEvaluator{}
	primary := phase4HTTPHandler(t, repositories, repository, evaluator, clock, oidc.Principal{ID: principalID.String(), TenantID: tenantID.String(), Issuer: "https://run010.test", Roles: []string{"agent-invoker"}})
	foreign := phase4HTTPHandler(t, repositories, foreignRepository, evaluator, clock, oidc.Principal{ID: foreignPrincipalID.String(), TenantID: foreignTenantID.String(), Issuer: "https://run010.test", Roles: []string{"agent-invoker"}})

	admissionBody := `{"objective":"exercise the complete lifecycle","constraints":{"max_execution_time_seconds":60,"max_llm_tokens":100}}`
	admission := phase4Request(t, primary, http.MethodPost, "/v1/agents/"+agentID.String()+"/runs", admissionBody, "run010-admission-key-0001")
	if admission.Code != http.StatusCreated || admission.Header().Get("Location") == "" {
		t.Fatalf("admission status=%d body=%s", admission.Code, admission.Body.String())
	}
	var created struct {
		ID            string `json:"id"`
		AgentID       string `json:"agent_id"`
		VersionDigest string `json:"version_digest"`
		State         string `json:"state"`
		StateVersion  int64  `json:"state_version"`
		Envelope      struct {
			Version   int64 `json:"version"`
			Resources []any `json:"resources"`
		} `json:"envelope"`
	}
	if err := json.Unmarshal(admission.Body.Bytes(), &created); err != nil || created.ID == "" || created.AgentID != agentID.String() || created.VersionDigest != digest || created.State != "ADMITTED" || created.StateVersion != 1 || created.Envelope.Version != 1 || created.Envelope.Resources == nil {
		t.Fatalf("OpenAPI admission response=%+v error=%v body=%s", created, err, admission.Body.String())
	}
	runID, err := domain.ParseID(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	replay := phase4Request(t, primary, http.MethodPost, "/v1/agents/"+agentID.String()+"/runs", admissionBody, "run010-admission-key-0001")
	if replay.Code != admission.Code || replay.Body.String() != admission.Body.String() || replay.Header().Get("Location") != admission.Header().Get("Location") {
		t.Fatalf("admission replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	conflict := phase4Request(t, primary, http.MethodPost, "/v1/agents/"+agentID.String()+"/runs", `{"objective":"different request"}`, "run010-admission-key-0001")
	if conflict.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	query := phase4Request(t, primary, http.MethodGet, "/v1/runs/"+runID.String(), "", "")
	if query.Code != http.StatusOK || !json.Valid(query.Body.Bytes()) || !strings.Contains(query.Body.String(), `"state":"ADMITTED"`) {
		t.Fatalf("run query status=%d body=%s", query.Code, query.Body.String())
	}
	isolated := phase4Request(t, foreign, http.MethodGet, "/v1/runs/"+runID.String(), "", "")
	if isolated.Code != http.StatusNotFound || !strings.Contains(isolated.Body.String(), `"code":"not_found"`) {
		t.Fatalf("cross-tenant query status=%d body=%s", isolated.Code, isolated.Body.String())
	}

	firstStream := phase4Stream(t, primary, "/v1/runs/"+runID.String()+"/events", "")
	firstCursor := phase4LastEventID(t, firstStream.Body.String())
	if firstStream.Code != http.StatusOK || !strings.Contains(firstStream.Body.String(), "event: run.admitted") {
		t.Fatalf("initial stream status=%d body=%s", firstStream.Code, firstStream.Body.String())
	}
	signalBody := `{"type":"CUSTOM","payload":{"name":"runtime.refresh","data":{"scope":"tools"}},"expected_state_version":1}`
	signal := phase4Request(t, primary, http.MethodPost, "/v1/runs/"+runID.String()+"/signals", signalBody, "run010-signal-key-000001")
	if signal.Code != http.StatusAccepted || !strings.Contains(signal.Body.String(), `"type":"run.signal.accepted"`) {
		t.Fatalf("signal status=%d body=%s", signal.Code, signal.Body.String())
	}
	signalReplay := phase4Request(t, primary, http.MethodPost, "/v1/runs/"+runID.String()+"/signals", signalBody, "run010-signal-key-000001")
	if signalReplay.Code != signal.Code || signalReplay.Body.String() != signal.Body.String() {
		t.Fatalf("signal replay status=%d body=%s", signalReplay.Code, signalReplay.Body.String())
	}
	resumed := phase4Stream(t, primary, "/v1/runs/"+runID.String()+"/events", firstCursor)
	if resumed.Code != http.StatusOK || strings.Contains(resumed.Body.String(), "event: run.admitted") || !strings.Contains(resumed.Body.String(), "event: run.signal.accepted") {
		t.Fatalf("resumed stream status=%d body=%s", resumed.Code, resumed.Body.String())
	}

	workerID := newE2EID(t)
	lease, err := repositories.ClaimRun(ctx, tenantID, workerID, newE2EID(t), now.Add(time.Second), now.Add(time.Minute))
	if err != nil || lease.RunID != runID {
		t.Fatalf("worker claim=%+v error=%v", lease, err)
	}
	if _, err := repositories.MutateWorkerRun(ctx, lease, domain.WorkerRunStart, newE2EID(t), now); err != nil {
		t.Fatal(err)
	}
	cancelBody := `{"reason_code":"caller.request","expected_state_version":2}`
	start := make(chan struct{})
	var cancelResponse *httptest.ResponseRecorder
	var workerErr error
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		<-start
		cancelResponse = phase4Request(t, primary, http.MethodPost, "/v1/runs/"+runID.String()+"/cancel", cancelBody, "run010-cancel-key-000001")
	}()
	go func() {
		defer group.Done()
		<-start
		_, workerErr = repositories.MutateWorkerRun(ctx, lease, domain.WorkerRunComplete, newE2EID(t), now)
	}()
	close(start)
	group.Wait()
	if cancelResponse.Code != http.StatusOK || workerErr != nil && domain.ErrorCodeOf(workerErr) != domain.CodeConflict {
		t.Fatalf("terminal race cancel_status=%d cancel_body=%s worker_error=%v", cancelResponse.Code, cancelResponse.Body.String(), workerErr)
	}
	cancelReplay := phase4Request(t, primary, http.MethodPost, "/v1/runs/"+runID.String()+"/cancel", cancelBody, "run010-cancel-key-000001")
	if cancelReplay.Code != cancelResponse.Code || cancelReplay.Body.String() != cancelResponse.Body.String() {
		t.Fatalf("cancel replay status=%d body=%s", cancelReplay.Code, cancelReplay.Body.String())
	}
	projection, err := repository.GetRun(ctx, runID)
	if err != nil || projection.Run.State != domain.RunCompleted && projection.Run.State != domain.RunCancelled || projection.Run.StateVersion != 3 {
		t.Fatalf("terminal projection=%+v error=%v", projection.Run, err)
	}
	var terminalEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM run_events WHERE tenant_id=$1 AND run_id=$2 AND event_type IN ('run.completed','run.cancelled')`, tenantID.String(), runID.String()).Scan(&terminalEvents); err != nil || terminalEvents != 1 {
		t.Fatalf("terminal events=%d error=%v", terminalEvents, err)
	}
}

type phase4PolicyEvaluator struct{}

func (phase4PolicyEvaluator) Decide(_ context.Context, input policy.Input) (policy.Result, error) {
	allowed := input.Subject.TenantID == input.Resource.TenantID && !input.SecurityState.HasGap
	if allowed {
		allowed = false
		for _, role := range input.Subject.Roles {
			if role == "agent-invoker" {
				allowed = true
				break
			}
		}
	}
	reason := "action.not_permitted"
	if allowed {
		reason = "run.access.allowed"
		if input.Action == "runs.create" {
			reason = "agent.invoke.allowed"
		}
	}
	resolved := map[string]any{}
	if allowed && input.Action == "runs.create" {
		for key, value := range input.RequestedConstraints {
			resolved[key] = value
		}
	}
	return policy.Result{Decision: policy.Decision{ContractVersion: policy.ContractVersion, DecisionID: input.DecisionID, Allow: allowed, ReasonCodes: []string{reason}, ResolvedConstraints: resolved, Obligations: []policy.Obligation{}}, Metadata: policy.Metadata{PolicyDigest: "sha256:" + strings.Repeat("f", 64), PolicyVersion: 1}}, nil
}

func phase4HTTPHandler(t *testing.T, repositories *Repositories, repository *TenantRepository, evaluator policy.Evaluator, clock domain.Clock, principal oidc.Principal) http.Handler {
	t.Helper()
	resolver, err := application.NewVersionResolver(repository, evaluator, clock)
	if err != nil {
		t.Fatal(err)
	}
	admissionService, err := application.NewRunAdmissionService(resolver, repository, clock)
	if err != nil {
		t.Fatal(err)
	}
	queryService, err := application.NewRunQuery(repository, evaluator, clock)
	if err != nil {
		t.Fatal(err)
	}
	signalService, err := application.NewRunSignalService(repository, evaluator, clock)
	if err != nil {
		t.Fatal(err)
	}
	cancelService, err := application.NewRunCancellationService(repository, evaluator, clock)
	if err != nil {
		t.Fatal(err)
	}
	eventService, err := application.NewRunEventStream(queryService, repository, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	idempotency, err := NewIdempotencyStore(repositories)
	if err != nil {
		t.Fatal(err)
	}
	verifier := e2eVerifier{principal: principal}
	securityState := policy.SecurityState{TenantPolicyEpoch: 1}
	admission, err := httpserver.RunAdmissionHandler(verifier, admissionService, idempotency, clock, httpserver.RunAdmissionHTTPConfig{AuthorityConstraints: map[string]any{"max_execution_time_seconds": float64(300), "max_llm_tokens": float64(1000)}, SecurityState: securityState, Lease: time.Minute, TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	query, _ := httpserver.RunQueryHandler(verifier, queryService, securityState)
	signal, _ := httpserver.RunSignalHandler(verifier, signalService, idempotency, clock, httpserver.RunSignalHTTPConfig{SecurityState: securityState, Lease: time.Minute, TTL: time.Hour})
	cancellation, _ := httpserver.RunCancellationHandler(verifier, cancelService, idempotency, clock, httpserver.RunCancellationHTTPConfig{SecurityState: securityState, Lease: time.Minute, TTL: time.Hour})
	cursor, err := domain.NewRunEventCursorCodec([]byte("run010-event-cursor-key-32-bytes!!"))
	if err != nil {
		t.Fatal(err)
	}
	events, err := httpserver.RunEventStreamHandler(verifier, eventService, cursor, securityState, httpserver.RunEventStreamOptions{HeartbeatInterval: time.Second, PollInterval: time.Millisecond, WriteTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	metricSet, _ := metrics.New(false, metrics.BuildInfo{})
	traceSet, _ := tracing.New(context.Background(), tracing.Config{Mode: "noop"})
	logger, _ := logging.New(io.Discard, "info")
	httpConfig := config.Defaults().HTTP
	server, err := httpserver.New(httpConfig, httpserver.Dependencies{Logger: logger, Metrics: metricSet, Tracing: traceSet, NewID: func() (string, error) { id, idErr := domain.NewID(); return id.String(), idErr }, RunAdmission: admission, RunQuery: query, RunSignal: signal, RunCancellation: cancellation, RunEvents: events})
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler()
}

func phase4Request(t *testing.T, handler http.Handler, method, target, body, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer valid")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func phase4Stream(t *testing.T, handler http.Handler, target, cursor string) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer valid")
	if cursor != "" {
		request.Header.Set("Last-Event-ID", cursor)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func phase4LastEventID(t *testing.T, body string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "id: ") {
			return strings.TrimPrefix(line, "id: ")
		}
	}
	t.Fatalf("SSE response has no event ID: %s", body)
	return ""
}

var _ policy.Evaluator = phase4PolicyEvaluator{}
