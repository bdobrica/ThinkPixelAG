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

func TestVersionResolutionEligibilitySnapshotsIsolationAndImmutability(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	migrator, _ := NewMigrator(ctx, connection, os.DirFS(projectMigrationsDir(t)))
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close(ctx)
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tenant, foreign, owner, sponsor, agentID := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, id := range []domain.ID{tenant, foreign} {
		if _, err := pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at)VALUES($1,$2,$2,$3,$3)`, id.String(), "reg004-"+id.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE tenant_id=$1`, tenant.String())
	})
	for _, id := range []domain.ID{owner, sponsor} {
		if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,'https://reg004.test',$3,'HUMAN',$4)`, id.String(), tenant.String(), id.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents(id,tenant_id,name,owner_principal_id,sponsor_principal_id,risk_class,created_at,updated_at)VALUES($1,$2,$3,$4,$5,'HIGH',$6,$6)`, agentID.String(), tenant.String(), "resolved-"+agentID.String(), owner.String(), sponsor.String(), now); err != nil {
		t.Fatal(err)
	}
	repositories, _ := NewRepositories(pool)
	repository, _ := repositories.ForTenant(tenant)
	foreignRepository, _ := repositories.ForTenant(foreign)
	createVersion := func(imageByte string, created time.Time) (domain.AgentVersion, string) {
		manifest, _ := domain.NewAgentManifest("registry.example/agent@sha256:"+strings.Repeat(imageByte, 64), nil, nil, nil, nil, domain.AgentLimits{})
		digest, _ := manifest.ContentDigest()
		version := domain.AgentVersion{ID: mustNewRepositoryID(t), TenantID: tenant, AgentID: agentID, CreatedBy: owner, ContentDigest: digest, ImageDigest: "sha256:" + strings.Repeat(imageByte, 64), Manifest: manifest, CreatedAt: created}
		if err := repository.RegisterAgentVersion(ctx, version, nil); err != nil {
			t.Fatal(err)
		}
		return version, digest
	}
	oldVersion, oldDigest := createVersion("a", now)
	newVersion, newDigest := createVersion("b", now.Add(time.Second))
	decide := func(version domain.AgentVersion, digest string, decision domain.AgentVersionDecision, created time.Time) domain.AgentVersionApproval {
		approval := domain.AgentVersionApproval{ID: mustNewRepositoryID(t), TenantID: tenant, AgentID: agentID, ActorPrincipalID: owner, PolicyDecisionID: mustNewRepositoryID(t), Decision: decision, ReasonCode: "registry.version.changed", CreatedAt: created}
		stored, err := repository.RecordAgentVersionDecision(ctx, approval, digest, mustNewRepositoryID(t), mustNewRepositoryID(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		return stored
	}
	oldApproval := decide(oldVersion, oldDigest, domain.DecisionApprove, now.Add(2*time.Second))
	_ = decide(oldVersion, oldDigest, domain.DecisionDeprecate, now.Add(3*time.Second))
	newApproval := decide(newVersion, newDigest, domain.DecisionApprove, now.Add(4*time.Second))
	automatic, err := repository.ListAgentVersionCandidates(ctx, agentID, "")
	if err != nil || len(automatic) != 1 || automatic[0].Version.ID != newVersion.ID || automatic[0].State != domain.AgentVersionApproved {
		t.Fatalf("automatic=%+v err=%v", automatic, err)
	}
	rollback, err := repository.ListAgentVersionCandidates(ctx, agentID, oldDigest)
	if err != nil || len(rollback) != 1 || rollback[0].Approval.ID != oldApproval.ID || rollback[0].State != domain.AgentVersionDeprecated {
		t.Fatalf("rollback=%+v err=%v", rollback, err)
	}
	if candidates, err := foreignRepository.ListAgentVersionCandidates(ctx, agentID, newDigest); err != nil || len(candidates) != 0 {
		t.Fatalf("foreign candidates=%+v err=%v", candidates, err)
	}

	runID := mustNewRepositoryID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO runs(id,tenant_id,agent_id,agent_version_id,requested_by,state,constraints,created_at,updated_at)VALUES($1,$2,$3,$4,$5,'PENDING','{}',$6,$6)`, runID.String(), tenant.String(), agentID.String(), newVersion.ID.String(), owner.String(), now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	decisionID := mustNewRepositoryID(t)
	resolution := domain.RunVersionResolution{RunID: runID, TenantID: tenant, AgentID: agentID, AgentVersionID: newVersion.ID, ApprovalID: newApproval.ID, AgentContentDigest: newDigest, PolicyBundleDigest: "sha256:" + strings.Repeat("f", 64), PolicyActivationVersion: 9, Mode: domain.ResolutionAutomatic, InvocationDecisionID: decisionID, ResolvedConstraints: map[string]any{"max_tokens": float64(10)}, ResolvedAt: now.Add(5 * time.Second)}
	if err := repository.PersistRunVersionResolution(ctx, resolution); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.DescribeRunVersionResolution(ctx, runID)
	if err != nil || stored.AgentVersionID != newVersion.ID || stored.InvocationDecisionID != decisionID {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	if err := repository.PersistRunVersionResolution(ctx, resolution); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("duplicate error=%v", err)
	}
	if _, err := foreignRepository.DescribeRunVersionResolution(ctx, runID); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("foreign describe=%v", err)
	}
	_, err = pool.Exec(ctx, `UPDATE run_version_resolutions SET policy_activation_version=10 WHERE run_id=$1`, runID.String())
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("immutable update=%v", err)
	}

	revokedRun := mustNewRepositoryID(t)
	if _, err := pool.Exec(ctx, `INSERT INTO runs(id,tenant_id,agent_id,agent_version_id,requested_by,state,constraints,created_at,updated_at)VALUES($1,$2,$3,$4,$5,'PENDING','{}',$6,$6)`, revokedRun.String(), tenant.String(), agentID.String(), newVersion.ID.String(), owner.String(), now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	_ = decide(newVersion, newDigest, domain.DecisionRevoke, now.Add(6*time.Second))
	resolution.RunID, resolution.ResolvedAt = revokedRun, now.Add(6*time.Second)
	if err := repository.PersistRunVersionResolution(ctx, resolution); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("revoked persistence error=%v", err)
	}
}
