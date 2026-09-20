//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
