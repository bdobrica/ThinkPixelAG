//go:build e2e

package postgres

import (
	"context"
	"errors"
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
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPhase3RegistrySecurityWorkflow composes the HTTP identity boundary,
// application services, policy cache, and real PostgreSQL repositories. The
// narrower adapter and Rego suites prove each component; this test proves the
// security properties survive their composition.
func TestPhase3RegistrySecurityWorkflow(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("THINKPIXELAG_TEST_DATABASE_URL is required for the Phase 3 end-to-end suite")
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
	ownerID, sponsorID, invokerID := newE2EID(t), newE2EID(t), newE2EID(t)
	for _, tenant := range []domain.ID{tenantID, foreignTenantID} {
		if _, err := pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, tenant.String(), "reg006-"+tenant.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		// The outbox publisher is global rather than tenant-bound. Do not leave
		// this workflow's unpublished evidence eligible for another test run.
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE tenant_id IN ($1,$2)`, tenantID.String(), foreignTenantID.String())
	})
	for _, principal := range []domain.ID{ownerID, sponsorID, invokerID} {
		if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES($1,$2,'https://reg006.test',$3,'HUMAN',$4)`, principal.String(), tenantID.String(), principal.String(), now); err != nil {
			t.Fatal(err)
		}
	}

	repositories, err := NewRepositories(pool)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := repositories.ForTenant(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	foreignRepository, err := repositories.ForTenant(foreignTenantID)
	if err != nil {
		t.Fatal(err)
	}

	agentService, _ := application.NewAgentRegistry(repository, clock)
	agentID := newE2EID(t)
	agent, err := agentService.Create(ctx, application.CreateAgent{ID: agentID, TenantID: tenantID, OwnerPrincipalID: ownerID, SponsorPrincipalID: sponsorID, Name: "phase3-" + agentID.String(), Description: "Phase 3 composed security workflow", RiskClass: domain.AgentRiskMedium})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := domain.NewAgentManifest("registry.example/phase3@sha256:"+strings.Repeat("a", 64), []string{"model-a"}, []string{"tool-a"}, nil, nil, domain.AgentLimits{})
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := manifest.ContentDigest()
	versionService, _ := application.NewAgentVersionRegistry(repository, clock)
	version, err := versionService.Register(ctx, application.RegisterAgentVersion{ID: newE2EID(t), TenantID: tenantID, AgentID: agentID, CreatedBy: ownerID, ContentDigest: digest, Image: manifest.Image, Models: manifest.Models, Tools: manifest.Tools, Limits: manifest.Limits})
	if err != nil {
		t.Fatal(err)
	}

	// Database enforcement must remain the last line of defense even if an
	// adapter or future administrative path attempts mutation directly.
	_, err = pool.Exec(ctx, `UPDATE agent_versions SET image_digest=$1 WHERE tenant_id=$2 AND id=$3`, "sha256:"+strings.Repeat("b", 64), tenantID.String(), version.ID.String())
	var databaseError *pgconn.PgError
	if err == nil || !errors.As(err, &databaseError) || databaseError.Code != "55000" {
		t.Fatalf("immutable version update error = %v", err)
	}

	active := &e2eActivePolicy{digest: "sha256:" + strings.Repeat("f", 64), version: 1, fresh: true}
	baseEvaluator := &e2ePolicyEvaluator{active: active}
	cachedEvaluator, err := policy.NewCachedEvaluator(baseEvaluator, newE2ECache(), active.metadata, 10*time.Second, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	authorizer, _ := application.NewPolicyAgentApprovalAuthorizer(cachedEvaluator, func() time.Time { return now })
	approvalService, _ := application.NewAgentApprovalRegistry(repository, authorizer, clock)
	decision := application.DecideAgentVersion{ID: newE2EID(t), TenantID: tenantID, AgentID: agentID, ActorPrincipalID: ownerID, RequestID: newE2EID(t), VersionDigest: digest, Decision: domain.DecisionApprove, ReasonCode: "registry.version.approved"}
	if _, err := approvalService.Decide(ctx, decision); domain.ErrorCodeOf(err) != domain.CodeForbidden {
		t.Fatalf("role-free approval error = %v", err)
	}
	decision.ID, decision.RequestID, decision.Roles = newE2EID(t), newE2EID(t), []string{"governance-admin"}
	approval, err := approvalService.Decide(ctx, decision)
	if err != nil {
		t.Fatal(err)
	}
	if approval.AgentVersionID != version.ID {
		t.Fatalf("approved version = %s, want %s", approval.AgentVersionID, version.ID)
	}

	if _, _, err := versionService.Describe(ctx, foreignTenantID, agentID, digest); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("foreign version description error = %v", err)
	}
	if candidates, err := foreignRepository.ListAgentVersionCandidates(ctx, agentID, ""); err != nil || len(candidates) != 0 {
		t.Fatalf("foreign candidates = %v, %v", candidates, err)
	}

	resolver, _ := application.NewVersionResolver(repository, cachedEvaluator, clock)
	resolve := application.ResolveAgentVersion{RunID: newE2EID(t), TenantID: tenantID, AgentID: agentID, PrincipalID: invokerID, RequestID: newE2EID(t), RequestedConstraints: map[string]any{"max_tokens": float64(80)}, AuthorityConstraints: map[string]any{"max_tokens": float64(50)}, SecurityState: policy.SecurityState{TenantPolicyEpoch: 1}}
	if _, err := resolver.Resolve(ctx, resolve); domain.ErrorCodeOf(err) != domain.CodeForbidden {
		t.Fatalf("role-free resolution error = %v", err)
	}
	resolve.Roles = []string{"agent-invoker"}
	resolved, err := resolver.Resolve(ctx, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.AgentVersionID != version.ID || resolved.ResolvedConstraints["max_tokens"] != float64(50) {
		t.Fatalf("resolution = %+v", resolved)
	}
	firstCalls := baseEvaluator.calls()
	resolve.RunID, resolve.RequestID = newE2EID(t), newE2EID(t)
	if _, err := resolver.Resolve(ctx, resolve); err != nil {
		t.Fatal(err)
	}
	if baseEvaluator.calls() != firstCalls {
		t.Fatal("equivalent authorization did not use the decision cache")
	}
	resolve.SecurityState.TenantPolicyEpoch++
	if _, err := resolver.Resolve(ctx, resolve); err != nil {
		t.Fatal(err)
	}
	if baseEvaluator.calls() != firstCalls+1 {
		t.Fatal("tenant policy epoch change did not invalidate the cached decision")
	}
	active.setVersion(2)
	if _, err := resolver.Resolve(ctx, resolve); err != nil {
		t.Fatal(err)
	}
	if baseEvaluator.calls() != firstCalls+2 {
		t.Fatal("policy activation change did not invalidate the cached decision")
	}

	if _, err := pool.Exec(ctx, `INSERT INTO runs(id,tenant_id,agent_id,agent_version_id,requested_by,state,constraints,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'PENDING','{}',$6,$6)`, resolved.RunID.String(), tenantID.String(), agentID.String(), version.ID.String(), invokerID.String(), now); err != nil {
		t.Fatal(err)
	}
	if err := resolver.Persist(ctx, resolved); err != nil {
		t.Fatal(err)
	}
	if _, err := foreignRepository.DescribeRunVersionResolution(ctx, resolved.RunID); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("foreign resolution description error = %v", err)
	}

	// The public boundary must deny before any catalog work when identity is
	// missing, invalid, or supplied through an untrusted forwarding header.
	discovery, _ := application.NewAgentDiscovery(repository, cachedEvaluator, clock)
	handler, err := httpserver.AgentDiscoveryHandler(e2eVerifier{principal: oidc.Principal{ID: invokerID.String(), TenantID: tenantID.String(), Issuer: "https://reg006.test", Roles: []string{"agent-invoker"}}}, discovery, mustE2ECursorCodec(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/agents", nil),
		func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
			r.Header.Set("Authorization", "Bearer invalid")
			return r
		}(),
		func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
			r.Header.Set("Authorization", "Bearer valid")
			r.Header.Set("X-Tenant-ID", foreignTenantID.String())
			return r
		}(),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("identity denial status = %d, body=%s", response.Code, response.Body.String())
		}
	}
	if agent.TenantID != tenantID { // Keep the successful tenant authority explicit in this workflow.
		t.Fatal("created agent crossed its authenticated tenant")
	}
}

type fixedE2EClock struct{ now time.Time }

func (c fixedE2EClock) Now() time.Time { return c.now }

func newE2EID(t *testing.T) domain.ID {
	t.Helper()
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type e2eVerifier struct{ principal oidc.Principal }

func (v e2eVerifier) Verify(_ context.Context, token string) (oidc.Principal, error) {
	if token != "valid" {
		return oidc.Principal{}, domain.NewError(domain.CodeUnauthenticated, "access token is invalid")
	}
	return v.principal, nil
}

func mustE2ECursorCodec(t *testing.T) *domain.CursorCodec {
	t.Helper()
	codec, err := domain.NewCursorCodec([]byte("reg006-cursor-integrity-key-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

type e2eActivePolicy struct {
	sync.RWMutex
	digest  string
	version int64
	fresh   bool
}

func (p *e2eActivePolicy) metadata() (string, int64, bool) {
	p.RLock()
	defer p.RUnlock()
	return p.digest, p.version, p.fresh
}

func (p *e2eActivePolicy) setVersion(version int64) {
	p.Lock()
	p.version = version
	p.Unlock()
}

type e2ePolicyEvaluator struct {
	mu     sync.Mutex
	count  int
	active *e2eActivePolicy
}

func (e *e2ePolicyEvaluator) calls() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.count
}

func (e *e2ePolicyEvaluator) Decide(_ context.Context, input policy.Input) (policy.Result, error) {
	e.mu.Lock()
	e.count++
	e.mu.Unlock()
	digest, version, fresh := e.active.metadata()
	if !fresh {
		return policy.Result{}, errors.New("active policy is stale")
	}
	allowed := false
	reason, ttl := "action.not_permitted", int64(0)
	roles := make(map[string]bool, len(input.Subject.Roles))
	for _, role := range input.Subject.Roles {
		roles[role] = true
	}
	if input.Subject.TenantID == input.Resource.TenantID && !input.SecurityState.HasGap && input.SecurityState.AgeSeconds <= 30 {
		switch input.Action {
		case "versions.approve":
			allowed = roles["governance-admin"] && input.SecurityState.Authoritative
			if allowed {
				reason = "governance.operation.allowed"
			}
		case "runs.create", "agents.list", "agents.describe":
			allowed = roles["agent-invoker"]
			if allowed {
				reason, ttl = "agent.invoke.allowed", 10
			}
			if allowed && strings.HasPrefix(input.Action, "agents.") {
				reason = "agent.discover.allowed"
			}
		}
	}
	resolved := map[string]any{}
	if allowed && input.Action == "runs.create" {
		requested, requestedOK := input.RequestedConstraints["max_tokens"].(float64)
		authority, authorityOK := input.AuthorityConstraints["max_tokens"].(float64)
		if requestedOK && authorityOK {
			resolved["max_tokens"] = min(requested, authority)
		}
	}
	decision := policy.Decision{ContractVersion: policy.ContractVersion, DecisionID: input.DecisionID, Allow: allowed, ReasonCodes: []string{reason}, ResolvedConstraints: resolved, Obligations: []policy.Obligation{}, DecisionTTLSeconds: ttl}
	return policy.Result{Decision: decision, Metadata: policy.Metadata{PolicyDigest: digest, PolicyVersion: version}}, nil
}

type e2eCache struct {
	mu     sync.Mutex
	values map[string][]byte
}

func newE2ECache() *e2eCache { return &e2eCache{values: make(map[string][]byte)} }

func (c *e2eCache) Get(_ context.Context, key string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.values[key]
	if !ok {
		return nil, errors.New("cache miss")
	}
	return append([]byte(nil), value...), nil
}

func (c *e2eCache) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	c.mu.Lock()
	c.values[key] = append([]byte(nil), value...)
	c.mu.Unlock()
	return nil
}
