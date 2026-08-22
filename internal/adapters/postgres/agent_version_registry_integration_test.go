//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentVersionRegistryContentAddressingIsolationAtomicityAndImmutability(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
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
	defer pool.Close()

	tenantA, tenantB := mustNewRepositoryID(t), mustNewRepositoryID(t)
	owner, sponsor := mustNewRepositoryID(t), mustNewRepositoryID(t)
	agentID, versionID := mustNewRepositoryID(t), mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, tenant := range []domain.ID{tenantA, tenantB} {
		slug := "reg002-" + tenant.String()
		if _, err := pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, tenant.String(), slug, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, principal := range []domain.ID{owner, sponsor} {
		if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES($1,$2,'https://reg002.test',$3,'HUMAN',$4)`, principal.String(), tenantA.String(), principal.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents(id,tenant_id,name,owner_principal_id,sponsor_principal_id,risk_class,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'HIGH',$6,$6)`, agentID.String(), tenantA.String(), "versioned-"+agentID.String(), owner.String(), sponsor.String(), now); err != nil {
		t.Fatal(err)
	}

	repositories, _ := NewRepositories(pool)
	repoA, _ := repositories.ForTenant(tenantA)
	repoB, _ := repositories.ForTenant(tenantB)
	image := "registry.example/agent@sha256:" + strings.Repeat("a", 64)
	longSkill := "skill.example/" + strings.Repeat("s", 600)
	manifest, err := domain.NewAgentManifest(image, []string{"model.a"}, []string{"tool.a"}, []string{longSkill}, []string{"agent.child"}, domain.AgentLimits{})
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := manifest.ContentDigest()
	version := domain.AgentVersion{ID: versionID, TenantID: tenantA, AgentID: agentID, CreatedBy: owner, ContentDigest: digest, ImageDigest: "sha256:" + strings.Repeat("a", 64), Manifest: manifest, CreatedAt: now}
	ids := []domain.ID{mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)}
	capabilities, _ := version.Capabilities(ids)
	if err := repoA.RegisterAgentVersion(ctx, version, capabilities); err != nil {
		t.Fatal(err)
	}
	stored, storedCapabilities, err := repoA.DescribeAgentVersion(ctx, agentID, digest)
	if err != nil || stored.ContentDigest != digest || len(storedCapabilities) != 4 {
		t.Fatalf("DescribeAgentVersion = %+v, %+v, %v", stored, storedCapabilities, err)
	}
	if _, _, err := repoB.DescribeAgentVersion(ctx, agentID, digest); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-tenant describe error = %v", err)
	}
	if err := repoA.RegisterAgentVersion(ctx, version, capabilities); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("duplicate registration error = %v", err)
	}

	for _, statement := range []string{
		`UPDATE agent_versions SET image_digest='sha256:` + strings.Repeat("b", 64) + `' WHERE id='` + versionID.String() + `'`,
		`DELETE FROM agent_versions WHERE id='` + versionID.String() + `'`,
		`UPDATE agent_capabilities SET capability_identifier='tampered' WHERE id='` + ids[0].String() + `'`,
		`DELETE FROM agent_capabilities WHERE id='` + ids[0].String() + `'`,
	} {
		_, err := pool.Exec(ctx, statement)
		var pgErr *pgconn.PgError
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "55000" {
			t.Fatalf("immutable mutation error = %v", err)
		}
	}

	secondManifest, _ := domain.NewAgentManifest("registry.example/agent@sha256:"+strings.Repeat("b", 64), nil, nil, nil, nil, domain.AgentLimits{})
	secondDigest, _ := secondManifest.ContentDigest()
	second := domain.AgentVersion{ID: mustNewRepositoryID(t), TenantID: tenantA, AgentID: agentID, CreatedBy: mustNewRepositoryID(t), ContentDigest: secondDigest, ImageDigest: "sha256:" + strings.Repeat("b", 64), Manifest: secondManifest, CreatedAt: now.Add(time.Second)}
	if err := repoA.RegisterAgentVersion(ctx, second, nil); domain.ErrorCodeOf(err) != domain.CodeInvalidArgument {
		t.Fatalf("invalid atomic registration error = %v", err)
	}
	if _, _, err := repoA.DescribeAgentVersion(ctx, agentID, secondDigest); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("failed registration left version behind: %v", err)
	}
}
