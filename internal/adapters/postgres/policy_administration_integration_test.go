//go:build integration

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/httpserver"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/localapprovals"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/localkeys"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/opa"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/artifact"
	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/logging"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/metrics"
	"github.com/bdobrica/ThinkPixelAG/internal/observability/tracing"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const administrationPolicySource = `package thinkpixelag.authorization
import rego.v1
decision := {"contract_version":"thinkpixelag.authorization/v1alpha1","decision_id":input.decision_id,"allow": "policy-admin" in input.subject.roles,"reason_codes":["governance.operation.allowed"],"resolved_constraints":{},"obligations":[],"decision_ttl_seconds":0}
`

type administrationFixture struct {
	pool          *pgxpool.Pool
	repo          *TenantRepository
	tenant, actor domain.ID
	key           *localkeys.Key
	modules       *opa.Modules
	service       *application.PolicyAdministration
	initial       ports.PolicyArtifact
}

func newAdministrationFixture(t *testing.T) *administrationFixture {
	t.Helper()
	dbURL, opaURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL"), os.Getenv("THINKPIXELAG_TEST_OPA_URL")
	if dbURL == "" || opaURL == "" {
		t.Skip("PostgreSQL and OPA integration URLs required")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := NewMigrator(ctx, conn, os.DirFS(projectMigrationsDir(t)))
	if err = m.Up(ctx); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tenant, actor := mustNewRepositoryID(t), mustNewRepositoryID(t)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE tenant_id=$1`, tenant.String())
	})
	now := time.Now().UTC().Add(-time.Second)
	if _, err = pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at)VALUES($1,$2,$2,$3,$3)`, tenant.String(), "admin-"+tenant.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,'https://admin.test',$4,'HUMAN',$3)`, actor.String(), tenant.String(), now, actor.String()); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "ag-administration-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	key, err := localkeys.Provision(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	modules := &opa.Modules{Base: opaURL, Client: &http.Client{Timeout: 3 * time.Second}, Timeout: 3 * time.Second}
	repositories, _ := NewRepositories(pool)
	repo, _ := repositories.ForTenant(tenant)
	tx, _ := NewTransactor(pool)
	fresh, _ := policy.NewFreshness(time.Minute, time.Now)
	store, _ := NewPolicyStore(pool, tx, fresh)
	digestToSign, digest, err := artifact.SigningDigest(artifact.PolicyBundle, policy.ContractVersion, 1, []byte(administrationPolicySource))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := key.Sign(ctx, key.ID(), digestToSign)
	if err != nil {
		t.Fatal(err)
	}
	b := PolicyBundle{ID: mustNewRepositoryID(t), TenantID: tenant, CreatedBy: actor, Channel: "stable", Digest: digest, ContractVersion: policy.ContractVersion, ArtifactRevision: 1, Bundle: []byte(administrationPolicySource), Signature: sig.Value, SignerKeyID: sig.KeyID, SignerKeyVersion: sig.KeyVersion, SignatureAlgorithm: sig.Algorithm, CreatedAt: now}
	if err = store.VerifyAndPersist(ctx, b, key, modules); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Activate(ctx, tenant, b.ID, actor, "stable", "policy.initial", now); err != nil {
		t.Fatal(err)
	}
	evaluator := &opa.ArtifactEvaluator{Store: repo, Modules: modules, Verifier: key, Channel: "stable", MaxTTL: time.Minute}
	service := &application.PolicyAdministration{Store: repo, Evaluator: evaluator, Modules: modules, Verifier: key, Signer: key, SigningKeyID: key.ID(), ApprovalProvider: &localapprovals.Provider{Store: repo}, Channel: "stable", Clock: domain.SystemClock{}}
	initial, err := repo.PolicyArtifact(ctx, "stable", digest)
	if err != nil {
		t.Fatal(err)
	}
	return &administrationFixture{pool: pool, repo: repo, tenant: tenant, actor: actor, key: key, modules: modules, service: service, initial: initial}
}

type administrationVerifier struct{ tenant, actor, approver domain.ID }

func (v administrationVerifier) Verify(_ context.Context, token string) (oidc.Principal, error) {
	roles := []string{"agent-invoker"}
	if token == "admin" || token == "approver" {
		roles = []string{"policy-admin"}
	}
	actor := v.actor
	if token == "approver" {
		actor = v.approver
	}
	return oidc.Principal{TenantID: v.tenant.String(), ID: actor.String(), Roles: roles}, nil
}
func administrationHTTP(t *testing.T, f *administrationFixture) http.Handler {
	return administrationHTTPWithApprover(t, f, domain.ID{})
}
func administrationHTTPWithApprover(t *testing.T, f *administrationFixture, approver domain.ID) http.Handler {
	codec, _ := domain.NewCursorCodec(bytes.Repeat([]byte{1}, 32))
	logger, _ := logging.New(io.Discard, "info")
	metric, _ := metrics.New(false, metrics.BuildInfo{})
	trace, _ := tracing.New(context.Background(), tracing.Config{Mode: "noop"})
	server, err := httpserver.New(config.Defaults().HTTP, httpserver.Dependencies{Logger: logger, Metrics: metric, Tracing: trace, NewID: func() (string, error) { id, err := domain.NewID(); return id.String(), err }, RegistryAdministration: httpserver.RegistryAdministrationHandler(administrationVerifier{f.tenant, f.actor, approver}, &application.RegistryAdministration{Policy: f.service, Store: f.repo}), PolicyAdministration: httpserver.PolicyAdministrationHandler(administrationVerifier{f.tenant, f.actor, approver}, f.service), PolicyEditor: httpserver.PolicyEditorHandler(administrationVerifier{f.tenant, f.actor, approver}, f.service, codec)})
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler()
}
func TestAdministrationHTTPPolicyPromotionAndArtifactBinding(t *testing.T) {
	f := newAdministrationFixture(t)
	ctx := context.Background()
	handler := administrationHTTP(t, f)
	source := []byte(strings.Replace(administrationPolicySource, "governance.operation.allowed", "agent.invoke.allowed", 1))
	d, digest, _ := artifact.SigningDigest(artifact.PolicyBundle, policy.ContractVersion, 2, source)
	sig, _ := f.key.Sign(ctx, f.key.ID(), d)
	upload := application.PolicyUpload{Digest: digest, ContractVersion: policy.ContractVersion, Revision: 2, Source: source, Signature: sig.Value, KeyID: sig.KeyID, KeyVersion: sig.KeyVersion, Algorithm: sig.Algorithm}
	call := func(path, key, token string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	if got := call("/v1/admin/policies", "policy-upload-denied", "invoker", upload); got.Code != 403 {
		t.Fatalf("role denial %d %s", got.Code, got.Body)
	}
	bad := upload
	bad.Signature = []byte("invalid")
	if got := call("/v1/admin/policies", "policy-upload-invalid", "admin", bad); got.Code != 400 {
		t.Fatalf("invalid signature %d %s", got.Code, got.Body)
	}
	first := call("/v1/admin/policies", "policy-upload-valid", "admin", upload)
	if first.Code != 201 {
		t.Fatalf("upload %d %s", first.Code, first.Body)
	}
	replay := call("/v1/admin/policies", "policy-upload-valid", "admin", upload)
	if replay.Code != 201 || replay.Body.String() != first.Body.String() {
		t.Fatalf("upload replay %d %s", replay.Code, replay.Body)
	}
	activate := application.PolicyActivate{Channel: "stable", Reason: "policy.activate", Approval: "not-required"}
	path := "/v1/admin/policies/" + digest + "/activations"
	activated := call(path, "policy-activation-valid", "admin", activate)
	if activated.Code != 201 {
		t.Fatalf("activate %d %s", activated.Code, activated.Body)
	}
	replay = call(path, "policy-activation-valid", "admin", activate)
	if replay.Code != 201 || replay.Body.String() != activated.Body.String() {
		t.Fatalf("activation replay %d %s", replay.Code, replay.Body)
	}
	caller := application.AdminCaller{TenantID: f.tenant, PrincipalID: f.actor, RequestID: mustNewRepositoryID(t), Roles: []string{"policy-admin"}, Key: "check-active-policy"}
	op, err := f.service.Authorize(ctx, caller, "policies.manage", digest, nil)
	if err != nil || op.PolicyDigest != digest || op.PolicyVersion != 2 {
		t.Fatalf("policy binding %v %+v", err, op)
	}
	// A new evaluator/replica reconciles directly from authority after restart.
	fresh := *f.service
	fresh.Evaluator = &opa.ArtifactEvaluator{Store: f.repo, Modules: f.modules, Verifier: f.key, Channel: "stable", MaxTTL: time.Minute}
	if op, err = fresh.Authorize(ctx, caller, "policies.manage", digest, nil); err != nil || op.PolicyVersion != 2 {
		t.Fatalf("restart binding %v", err)
	}
	rollback := call("/v1/admin/policies/"+f.initial.Digest+"/activations", "rollback-no-approval", "admin", activate)
	if rollback.Code != 403 {
		t.Fatalf("unapproved rollback %d %s", rollback.Code, rollback.Body)
	}
	var audits, outbox int
	if err = f.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM audit_events WHERE tenant_id=$1),(SELECT count(*) FROM outbox_messages WHERE tenant_id=$1)`, f.tenant.String()).Scan(&audits, &outbox); err != nil || audits != 2 || outbox != 2 {
		t.Fatalf("atomic replay/evidence audits=%d outbox=%d err=%v", audits, outbox, err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE policy_bundles SET bundle='changed' WHERE tenant_id=$1`, f.tenant.String()); err == nil {
		t.Fatal("mutated signed artifact")
	}
}
