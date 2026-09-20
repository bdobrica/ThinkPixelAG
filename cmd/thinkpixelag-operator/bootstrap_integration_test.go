//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/localkeys"
	opaadapter "github.com/bdobrica/ThinkPixelAG/internal/adapters/opa"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBootstrapCommandReplayRestartAndNoReplacement(t *testing.T) {
	db, opa := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL"), os.Getenv("THINKPIXELAG_TEST_OPA_URL")
	if db == "" || opa == "" {
		t.Skip("PostgreSQL and OPA required")
	}
	t.Setenv("THINKPIXELAG_ENVIRONMENT", "local")
	t.Setenv("THINKPIXELAG_DATABASE_URL", db)
	ctx := context.Background()
	conn, e := pgx.Connect(ctx, db)
	if e != nil {
		t.Fatal(e)
	}
	m, e := postgres.NewMigrator(ctx, conn, os.DirFS("../../migrations"))
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Up(ctx); e != nil {
		t.Fatal(e)
	}
	conn.Close(ctx)
	id := func() domain.ID {
		v, e := domain.NewID()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	seconds := int64(300)
	tokens := int64(1000)
	manifest, e := domain.NewAgentManifest("registry.example/evaluation@sha256:"+strings.Repeat("a", 64), nil, nil, nil, nil, domain.AgentLimits{MaxExecutionTimeSeconds: &seconds, MaxLLMTokens: &tokens})
	if e != nil {
		t.Fatal(e)
	}
	b := ports.BootstrapSpec{TenantID: id(), Slug: "bootstrap-" + id().String(), Issuer: "https://operator.test", Principals: []domain.ID{id(), id()}, Mappings: map[string]string{"operators": "policy-admin", "registrars": "registry-admin", "users": "agent-invoker"}, OPA: ports.OPAConnection{Endpoint: opa}, Channel: "stable", AgentID: id(), AgentName: "evaluation", Manifest: manifest}
	dir, e := os.MkdirTemp("", "ag-operator-test-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	spec := filepath.Join(dir, "spec.json")
	raw, _ := json.Marshal(b)
	if e = os.WriteFile(spec, raw, 0600); e != nil {
		t.Fatal(e)
	}
	args := []string{"bootstrap", "--spec", spec, "--policy", "../../policies/authorization.rego", "--key", filepath.Join(dir, "key"), "--opa-origin", opa}
	var first, again bytes.Buffer
	if e = run(ctx, args, &first); e != nil {
		t.Fatal(e)
	}
	// A second invocation opens fresh key/database handles, like process restart.
	if e = run(ctx, args, &again); e != nil || again.String() != first.String() {
		t.Fatal("restart/replay", e)
	}
	var result ports.BootstrapResult
	if e = json.Unmarshal(first.Bytes(), &result); e != nil || result.TenantID != b.TenantID || result.AgentID != b.AgentID {
		t.Fatal(result, e)
	}
	pool, e := pgxpool.New(ctx, db)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	defer pool.Exec(ctx, `DELETE FROM outbox_messages WHERE tenant_id=$1`, b.TenantID.String())
	repos, _ := postgres.NewRepositories(pool)
	repo, _ := repos.ForTenant(b.TenantID)
	state, e := repo.AgentVersionEligibility(ctx, b.AgentID, result.VersionDigest)
	if e != nil || state != domain.AgentVersionApproved {
		t.Fatal(state, e)
	}
	var dimensions int
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM resource_dimensions WHERE tenant_id=$1`, b.TenantID.String()).Scan(&dimensions); e != nil || dimensions != 7 {
		t.Fatal("bootstrap accounting catalog", dimensions, e)
	}
	// Simulate an installation bootstrapped before resource definitions existed.
	if _, e = pool.Exec(ctx, `DELETE FROM resource_dimensions WHERE tenant_id=$1`, b.TenantID.String()); e != nil {
		t.Fatal(e)
	}
	var repaired bytes.Buffer
	if e = run(ctx, append(args, "--repair-resource-catalog"), &repaired); e != nil || repaired.String() != first.String() {
		t.Fatal("explicit catalog repair", e)
	}
	if e = pool.QueryRow(ctx, `SELECT count(*) FROM resource_dimensions WHERE tenant_id=$1`, b.TenantID.String()).Scan(&dimensions); e != nil || dimensions != 7 {
		t.Fatal("repaired catalog", dimensions, e)
	}
	key, e := localkeys.Open(filepath.Join(dir, "key"))
	if e != nil {
		t.Fatal(e)
	}
	evaluator := &opaadapter.ArtifactEvaluator{Store: repo, Modules: &opaadapter.Modules{Base: opa, Client: http.DefaultClient, Timeout: time.Second}, Verifier: key, Channel: "stable", MaxTTL: time.Minute}
	resolver, e := application.NewVersionResolver(repo, evaluator, domain.SystemClock{})
	if e != nil {
		t.Fatal(e)
	}
	admission, e := application.NewRunAdmissionService(resolver, repo, domain.SystemClock{})
	if e != nil {
		t.Fatal(e)
	}
	admitted, e := admission.Admit(ctx, application.AdmitRun{TenantID: b.TenantID, PrincipalID: b.Principals[0], AgentID: b.AgentID, RequestID: id(), Roles: []string{"agent-invoker"}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{"max_execution_time_seconds": int64(300), "max_llm_tokens": int64(1000)}, SecurityState: policy.SecurityState{Authoritative: true}})
	if e != nil || admitted.DeadlineAt == nil {
		t.Fatal("provisioned tenant cannot admit a bounded Run", e)
	}
	b.AgentName = "replacement"
	raw, _ = json.Marshal(b)
	if e = os.WriteFile(spec, raw, 0600); e != nil {
		t.Fatal(e)
	}
	var rejected bytes.Buffer
	if e = run(ctx, args, &rejected); e == nil || rejected.Len() != 0 {
		t.Fatal("changed bootstrap replaced authority", e)
	}
	if strings.Contains(first.String(), "signature") || strings.Contains(first.String(), "token") || strings.Contains(first.String(), "key") {
		t.Fatal("sensitive bootstrap output")
	}
}
