//go:build e2e

package postgres

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/httpserver"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	phase6PropagationSamples = 100
	phase6PropagationSLO     = 5 * time.Second
)

// TestPhase6ConnectedGatewayPropagationSLO measures the complete local-region
// path from the start of an authoritative PostgreSQL revocation transaction to
// receipt through the authenticated gateway SSE endpoint. Starting before the
// transaction makes the measurement conservative: it includes commit latency,
// polling, encoding, HTTP delivery, and client decoding.
func TestPhase6ConnectedGatewayPropagationSLO(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("THINKPIXELAG_TEST_DATABASE_URL is required for the Phase 6 end-to-end suite")
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
	tenant, gateway := newE2EID(t), newE2EID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, tenant.String(), "rev011-"+tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES($1,$2,'https://rev011.test',$3,'WORKLOAD',$4)`, gateway.String(), tenant.String(), gateway.String(), now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE tenant_id=$1`, tenant.String())
	})

	repositories, err := NewRepositories(pool)
	if err != nil {
		t.Fatal(err)
	}
	tenantRepository, err := repositories.ForTenant(tenant)
	if err != nil {
		t.Fatal(err)
	}
	distribution, err := application.NewRevocationDistribution(repositories, phase6GatewayEvaluator{}, domain.SystemClock{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cursorCodec, err := domain.NewRevocationCursorCodec([]byte("rev011-cursor-integrity-key-32bytes"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := httpserver.RevocationDistributionHandler(
		e2eVerifier{principal: oidc.Principal{ID: gateway.String(), TenantID: tenant.String(), Issuer: "https://rev011.test", Roles: []string{"trusted-gateway"}}},
		distribution,
		cursorCodec,
		httpserver.RevocationStreamOptions{HeartbeatInterval: 10 * time.Millisecond, PollInterval: 5 * time.Millisecond, WriteTimeout: time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	streamContext, cancelStream := context.WithCancel(ctx)
	t.Cleanup(cancelStream)
	request, err := http.NewRequestWithContext(streamContext, http.MethodGet, server.URL+"/v1/trusted/revocations/events?after_sequence=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer valid")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stream status=%d", response.StatusCode)
	}

	received := make(chan phase6GatewayReceipt, phase6PropagationSamples)
	streamErrors := make(chan error, 1)
	go readPhase6GatewayEvents(response, received, streamErrors)

	started := make(map[string]time.Time, phase6PropagationSamples)
	for i := 0; i < phase6PropagationSamples; i++ {
		revocationID := newE2EID(t)
		started[revocationID.String()] = time.Now()
		revocation := domain.Revocation{
			ID: revocationID, TenantID: &tenant, ActorPrincipalID: gateway,
			Scope: domain.RevocationToolID, Target: fmt.Sprintf("rev011-tool-%03d", i),
			ReasonCode: "security.compromise", EffectiveAt: time.Now().UTC(), CreatedAt: time.Now().UTC(),
		}
		if _, err := tenantRepository.CreateRevocation(ctx, revocation, phase6RevocationEvidence(t)); err != nil {
			t.Fatal(err)
		}
	}

	latencies := make([]time.Duration, 0, phase6PropagationSamples)
	deadline := time.NewTimer(phase6PropagationSLO + 5*time.Second)
	defer deadline.Stop()
	for len(latencies) < phase6PropagationSamples {
		select {
		case receipt := <-received:
			start, ok := started[receipt.revocationID]
			if ok {
				latencies = append(latencies, receipt.receivedAt.Sub(start))
				delete(started, receipt.revocationID)
			}
		case streamErr := <-streamErrors:
			t.Fatalf("read gateway stream: %v", streamErr)
		case <-deadline.C:
			t.Fatalf("received %d/%d revocations before timeout", len(latencies), phase6PropagationSamples)
		}
	}
	cancelStream()
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p50, p99, maximum := latencies[49], latencies[98], latencies[len(latencies)-1]
	t.Logf("connected gateway propagation samples=%d p50=%s p99=%s max=%s target_p99=%s", len(latencies), p50, p99, maximum, phase6PropagationSLO)
	if p99 > phase6PropagationSLO {
		t.Fatalf("connected gateway propagation p99=%s exceeds %s", p99, phase6PropagationSLO)
	}
}

type phase6GatewayEvaluator struct{}

func (phase6GatewayEvaluator) Decide(_ context.Context, in policy.Input) (policy.Result, error) {
	allow := false
	for _, role := range in.Subject.Roles {
		allow = allow || role == "trusted-gateway"
	}
	return policy.Result{Decision: policy.Decision{ContractVersion: policy.ContractVersion, DecisionID: in.DecisionID, Allow: allow, ReasonCodes: []string{"workload.operation.allowed"}, ResolvedConstraints: map[string]any{}, Obligations: []policy.Obligation{}}}, nil
}

type phase6GatewayReceipt struct {
	revocationID string
	receivedAt   time.Time
}

func readPhase6GatewayEvents(response *http.Response, receipts chan<- phase6GatewayReceipt, failures chan<- error) {
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Revocation struct {
				ID string `json:"id"`
			} `json:"revocation"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			failures <- err
			return
		}
		receipts <- phase6GatewayReceipt{revocationID: event.Revocation.ID, receivedAt: time.Now()}
	}
	if err := scanner.Err(); err != nil && !strings.Contains(err.Error(), "context canceled") {
		failures <- err
	}
}

func phase6RevocationEvidence(t *testing.T) ports.RevocationEvidence {
	t.Helper()
	return ports.RevocationEvidence{
		ChangeID: newE2EID(t), EventID: newE2EID(t), AuditID: newE2EID(t), OutboxID: newE2EID(t),
		RequestID: newE2EID(t), PolicyDecisionID: newE2EID(t), ReasonCodes: []string{"revocation.allowed"},
	}
}
