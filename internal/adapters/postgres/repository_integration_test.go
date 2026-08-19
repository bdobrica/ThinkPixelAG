package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTenantRepositoriesRollbackAndIsolation(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	migrationConnection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	migrator, err := NewMigrator(ctx, migrationConnection, os.DirFS(projectMigrationsDir(t)))
	if err != nil {
		migrationConnection.Close(ctx)
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		migrationConnection.Close(ctx)
		t.Fatal(err)
	}
	if err := migrationConnection.Close(ctx); err != nil {
		t.Fatal(err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tenantA, tenantB := mustNewRepositoryID(t), mustNewRepositoryID(t)
	principalA, principalB := mustNewRepositoryID(t), mustNewRepositoryID(t)
	rolledBackPrincipal := mustNewRepositoryID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, fixture := range []struct {
		tenant, principal domain.ID
	}{
		{tenantA, principalA}, {tenantB, principalB},
	} {
		slug := "data008-" + fixture.tenant.String()
		if _, err := pool.Exec(ctx, `INSERT INTO tenants (id, slug, display_name, created_at, updated_at) VALUES ($1, $2, $3, $4, $4)`, fixture.tenant.String(), slug, slug, now); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO principals (id, tenant_id, external_issuer, external_subject, principal_type, created_at) VALUES ($1, $2, 'https://data008.test', $3, 'HUMAN', $4)`, fixture.principal.String(), fixture.tenant.String(), fixture.principal.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM principals WHERE id = ANY($1::uuid[])`, []string{principalA.String(), principalB.String(), rolledBackPrincipal.String()})
		_, _ = pool.Exec(context.Background(), `DELETE FROM tenants WHERE id = ANY($1::uuid[])`, []string{tenantA.String(), tenantB.String()})
	})

	repositories, err := NewRepositories(pool)
	if err != nil {
		t.Fatal(err)
	}
	tenantARepository, err := repositories.ForTenant(tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tenantARepository.FindIdentity(ctx, RecordPrincipal, principalA); err != nil {
		t.Fatalf("same-tenant lookup failed: %v", err)
	}
	if _, err := tenantARepository.FindIdentity(ctx, RecordPrincipal, principalB); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("cross-tenant lookup error = %v, want ErrRecordNotFound", err)
	}

	transactor, err := NewTransactor(pool)
	if err != nil {
		t.Fatal(err)
	}
	rollbackCause := errors.New("force repository rollback")
	err = transactor.WithinTransaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(ctx context.Context, tx DBTX) error {
		txRepositories, err := repositories.WithDB(tx)
		if err != nil {
			return err
		}
		txTenantA, err := txRepositories.ForTenant(tenantA)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, tenant_id, external_issuer, external_subject, principal_type, created_at) VALUES ($1, $2, 'https://data008.test', $3, 'HUMAN', $4)`, rolledBackPrincipal.String(), tenantA.String(), rolledBackPrincipal.String(), now); err != nil {
			return err
		}
		if _, err := txTenantA.FindIdentity(ctx, RecordPrincipal, rolledBackPrincipal); err != nil {
			return fmt.Errorf("transaction-bound repository lookup: %w", err)
		}
		return rollbackCause
	})
	if !errors.Is(err, rollbackCause) {
		t.Fatalf("WithinTransaction error = %v, want rollback cause", err)
	}
	if _, err := tenantARepository.FindIdentity(ctx, RecordPrincipal, rolledBackPrincipal); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("rolled-back record lookup error = %v, want ErrRecordNotFound", err)
	}
}
