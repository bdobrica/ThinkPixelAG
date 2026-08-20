//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAgentRegistryTenantIsolationLifecycleAndOptimisticUpdate(t *testing.T) {
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
	owner, sponsor, disabled := mustNewRepositoryID(t), mustNewRepositoryID(t), mustNewRepositoryID(t)
	agentID := mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, tenant := range []domain.ID{tenantA, tenantB} {
		slug := "reg001-" + tenant.String()
		if _, err := pool.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, tenant.String(), slug, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, principal := range []struct {
		id       domain.ID
		disabled bool
	}{{owner, false}, {sponsor, false}, {disabled, true}} {
		var disabledAt any
		if principal.disabled {
			disabledAt = now
		}
		if _, err := pool.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,disabled_at,created_at) VALUES($1,$2,'https://reg001.test',$3,'HUMAN',$4,$5)`, principal.id.String(), tenantA.String(), principal.id.String(), disabledAt, now); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM agents WHERE tenant_id=$1`, tenantA.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM principals WHERE tenant_id=$1`, tenantA.String())
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = ANY($1::uuid[])`, []string{tenantA.String(), tenantB.String()})
	})
	repositories, _ := NewRepositories(pool)
	repoA, _ := repositories.ForTenant(tenantA)
	repoB, _ := repositories.ForTenant(tenantB)
	eligible, err := repoA.PrincipalEligibility(ctx, owner)
	if err != nil || !eligible.Exists || eligible.Disabled {
		t.Fatalf("owner eligibility = %+v, %v", eligible, err)
	}
	eligible, err = repoA.PrincipalEligibility(ctx, disabled)
	if err != nil || !eligible.Disabled {
		t.Fatalf("disabled eligibility = %+v, %v", eligible, err)
	}

	agent := domain.Agent{ID: agentID, TenantID: tenantA, Name: "payments-review", Description: "review", OwnerPrincipalID: owner, SponsorPrincipalID: sponsor, RiskClass: domain.AgentRiskHigh, Status: domain.AgentActive, CreatedAt: now, UpdatedAt: now}
	if err := repoA.CreateAgent(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if _, err := repoB.DescribeAgent(ctx, agentID); domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("cross-tenant describe = %v", err)
	}
	agents, err := repoA.ListAgents(ctx, ports.AgentListQuery{Limit: 20})
	if err != nil || len(agents) != 1 || agents[0].ID != agentID {
		t.Fatalf("list = %+v, %v", agents, err)
	}
	agent.Description, agent.Status, agent.UpdatedAt = "updated", domain.AgentSuspended, now.Add(time.Second)
	if err := repoA.UpdateAgent(ctx, agent, now); err != nil {
		t.Fatal(err)
	}
	if err := repoA.UpdateAgent(ctx, agent, now); domain.ErrorCodeOf(err) != domain.CodeConflict {
		t.Fatalf("stale update = %v", err)
	}
	stored, err := repoA.DescribeAgent(ctx, agentID)
	if err != nil || stored.Description != "updated" || stored.Status != domain.AgentSuspended {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
}
