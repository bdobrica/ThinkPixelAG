//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentApprovalLifecycleEvidenceIsolationConcurrencyAndImmutability(t *testing.T) {
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

	tenant, foreign := mustNewRepositoryID(t), mustNewRepositoryID(t)
	actor, sponsor, agentID, versionID := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, id := range []domain.ID{tenant, foreign} {
		if _, err := pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, id.String(), "reg003-"+id.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_messages WHERE tenant_id=$1`, tenant.String())
	})
	for _, id := range []domain.ID{actor, sponsor} {
		if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at) VALUES($1,$2,'https://reg003.test',$3,'HUMAN',$4)`, id.String(), tenant.String(), id.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agents(id,tenant_id,name,owner_principal_id,sponsor_principal_id,risk_class,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'HIGH',$6,$6)`, agentID.String(), tenant.String(), "approved-"+agentID.String(), actor.String(), sponsor.String(), now); err != nil {
		t.Fatal(err)
	}
	manifest, _ := domain.NewAgentManifest("registry.example/agent@sha256:"+strings.Repeat("a", 64), nil, nil, nil, nil, domain.AgentLimits{})
	digest, _ := manifest.ContentDigest()
	repositories, _ := NewRepositories(pool)
	repository, _ := repositories.ForTenant(tenant)
	foreignRepository, _ := repositories.ForTenant(foreign)
	version := domain.AgentVersion{ID: versionID, TenantID: tenant, AgentID: agentID, CreatedBy: actor, ContentDigest: digest, ImageDigest: "sha256:" + strings.Repeat("a", 64), Manifest: manifest, CreatedAt: now}
	if err := repository.RegisterAgentVersion(ctx, version, nil); err != nil {
		t.Fatal(err)
	}
	if state, err := repository.AgentVersionEligibility(ctx, agentID, digest); err != nil || state != domain.AgentVersionRegistered {
		t.Fatalf("initial state=%s err=%v", state, err)
	}
	if _, err := foreignRepository.AgentVersionEligibility(ctx, agentID, digest); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("foreign state error=%v", err)
	}

	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		decision := domain.DecisionApprove
		approvalID, policyID, auditID, outboxID := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
		wg.Add(1)
		go func(decision domain.AgentVersionDecision) {
			defer wg.Done()
			approval := domain.AgentVersionApproval{ID: approvalID, TenantID: tenant, AgentID: agentID, ActorPrincipalID: actor, PolicyDecisionID: policyID, Decision: decision, ReasonCode: "registry.version.decided", CreatedAt: now.Add(time.Second)}
			_, err := repository.RecordAgentVersionDecision(ctx, approval, digest, auditID, outboxID, nil)
			results <- err
		}(decision)
	}
	wg.Wait()
	close(results)
	succeeded, conflicted := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if domain.ErrorCodeOf(err) == domain.CodeConflict {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent results success=%d conflict=%d", succeeded, conflicted)
	}
	for index, decision := range []domain.AgentVersionDecision{domain.DecisionDeprecate, domain.DecisionRevoke} {
		approval := domain.AgentVersionApproval{ID: mustNewRepositoryID(t), TenantID: tenant, AgentID: agentID, ActorPrincipalID: actor, PolicyDecisionID: mustNewRepositoryID(t), Decision: decision, ReasonCode: "registry.version.changed", CreatedAt: now.Add(time.Duration(index+2) * time.Second)}
		stored, err := repository.RecordAgentVersionDecision(ctx, approval, digest, mustNewRepositoryID(t), mustNewRepositoryID(t), nil)
		if err != nil || stored.AgentVersionID != versionID {
			t.Fatalf("decision %s = %+v, %v", decision, stored, err)
		}
	}
	if state, err := repository.AgentVersionEligibility(ctx, agentID, digest); err != nil || state != domain.AgentVersionRevoked {
		t.Fatalf("final state=%s err=%v", state, err)
	}
	var approvals, audits, messages int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_version_approvals WHERE tenant_id=$1),(SELECT count(*) FROM audit_events WHERE tenant_id=$1 AND resource_id=$2),(SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2)`, tenant.String(), digest).Scan(&approvals, &audits, &messages); err != nil {
		t.Fatal(err)
	}
	if approvals != 3 || audits != 3 || messages != 3 {
		t.Fatalf("evidence counts approvals=%d audits=%d outbox=%d", approvals, audits, messages)
	}
	_, err = pool.Exec(ctx, `UPDATE agent_version_approvals SET reason_code='tampered' WHERE tenant_id=$1`, tenant.String())
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("append-only error=%v", err)
	}
}
