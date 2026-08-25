package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMigrationQualification(t *testing.T) {
	databaseURL := os.Getenv("THINKPIXELAG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("THINKPIXELAG_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	sources := os.DirFS(projectMigrationsDir(t))

	t.Run("empty database and access paths", func(t *testing.T) {
		conn := newMigrationTestDatabase(t, databaseURL)
		migrateAndRequireVersion(t, ctx, conn, sources, 9)
		assertQualifiedSchema(t, ctx, conn)
	})

	t.Run("upgrade seeded prior fixture", func(t *testing.T) {
		conn := newMigrationTestDatabase(t, databaseURL)
		prior := migrationPrefixFixture(t, sources, 8)
		migrateAndRequireVersion(t, ctx, conn, prior, 8)

		tenantID := mustMigrationID(t)
		now := time.Now().UTC().Truncate(time.Microsecond)
		if _, err := conn.Exec(ctx, `INSERT INTO tenants (id, slug, display_name, created_at, updated_at) VALUES ($1, $2, 'prior fixture', $3, $3)`, tenantID, "prior-"+strings.ReplaceAll(tenantID, "-", ""), now); err != nil {
			t.Fatal(err)
		}
		migrateAndRequireVersion(t, ctx, conn, sources, 9)
		var displayName string
		if err := conn.QueryRow(ctx, `SELECT display_name FROM tenants WHERE id = $1`, tenantID).Scan(&displayName); err != nil {
			t.Fatalf("prior row was not preserved: %v", err)
		}
		if displayName != "prior fixture" {
			t.Fatalf("preserved display name = %q", displayName)
		}
		assertTableExists(t, ctx, conn, "outbox_messages")
	})

	t.Run("forward recovery after transactional failure", func(t *testing.T) {
		conn := newMigrationTestDatabase(t, databaseURL)
		broken := fstest.MapFS{
			"001_base.sql": {Data: []byte("CREATE TABLE recovery_base (id bigint PRIMARY KEY);\n---- create above / drop below ----\nDROP TABLE recovery_base;")},
			"002_next.sql": {Data: []byte("CREATE TABLE recovery_partial (id bigint);\nSELECT definitely_invalid_migration_syntax;\n---- create above / drop below ----\nDROP TABLE recovery_partial;")},
		}
		migrator, err := NewMigrator(ctx, conn, broken)
		if err != nil {
			t.Fatal(err)
		}
		if err := migrator.Up(ctx); err == nil {
			t.Fatal("broken migration unexpectedly succeeded")
		}
		version, err := migrator.Version(ctx)
		if err != nil || version != 1 {
			t.Fatalf("version after failed migration = %d, %v; want 1", version, err)
		}
		if relationExists(t, ctx, conn, "recovery_partial") {
			t.Fatal("failed migration left a partial table")
		}

		fixed := fstest.MapFS{
			"001_base.sql": broken["001_base.sql"],
			"002_next.sql": {Data: []byte("CREATE TABLE recovery_complete (id bigint PRIMARY KEY);\n---- create above / drop below ----\nDROP TABLE recovery_complete;")},
		}
		migrateAndRequireVersion(t, ctx, conn, fixed, 2)
		assertTableExists(t, ctx, conn, "recovery_complete")
	})
}

func assertQualifiedSchema(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	for _, table := range []string{"tenants", "runs", "resource_reservations", "revocation_log", "outbox_messages"} {
		assertTableExists(t, ctx, conn, table)
	}

	id := mustMigrationID(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err := conn.Exec(ctx, `INSERT INTO tenants (id, slug, display_name, created_at, updated_at) VALUES ($1, 'INVALID SLUG', 'invalid', $2, $2)`, id, now)
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("tenant check constraint error = %v", err)
	}

	tenantID := mustMigrationID(t)
	slug := "index-" + strings.ReplaceAll(tenantID, "-", "")
	if _, err := conn.Exec(ctx, `INSERT INTO tenants (id, slug, display_name, created_at, updated_at) VALUES ($1, $2, 'index fixture', $3, $3)`, tenantID, slug, now); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, `INSERT INTO tenants (id, slug, display_name, created_at, updated_at) VALUES ($1, $2, 'duplicate', $3, $3)`, mustMigrationID(t), slug, now)
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("tenant unique constraint error = %v", err)
	}
	_, err = conn.Exec(ctx, `INSERT INTO principals (id, tenant_id, external_issuer, external_subject, principal_type, created_at) VALUES ($1, $2, 'https://data011.test', 'missing-tenant', 'HUMAN', $3)`, mustMigrationID(t), mustMigrationID(t), now)
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("principal foreign-key constraint error = %v", err)
	}
	for name, statement := range map[string]string{
		"noncanonical unit":  `INSERT INTO resource_dimensions (id, tenant_id, name, class, unit, scale, minimum_value, maximum_value, aggregation, created_at) VALUES ($1, $2, 'tokens', 'CONSUMABLE', 'Tokens', 0, 0, 10, 'SUM', $3)`,
		"structural sum":     `INSERT INTO resource_dimensions (id, tenant_id, name, class, unit, scale, minimum_value, maximum_value, aggregation, created_at) VALUES ($1, $2, 'children', 'STRUCTURAL', 'children', 0, 0, 10, 'SUM', $3)`,
		"ambiguous deadline": `INSERT INTO resource_dimensions (id, tenant_id, name, class, unit, scale, minimum_value, maximum_value, aggregation, created_at) VALUES ($1, $2, 'deadline', 'DEADLINE', 'seconds', 0, 0, 10, 'ABSOLUTE', $3)`,
	} {
		_, err = conn.Exec(ctx, statement, mustMigrationID(t), tenantID, now)
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("%s check constraint error = %v", name, err)
		}
	}
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	assertPlanUsesIndex(t, ctx, conn, `SELECT id FROM agents WHERE tenant_id = $1 AND status = 'ACTIVE' ORDER BY id LIMIT 20`, tenantID, "agents_tenant_status_idx")
	assertPlanUsesIndex(t, ctx, conn, `SELECT id FROM outbox_messages WHERE published_at IS NULL AND dead_lettered_at IS NULL AND available_at <= now() ORDER BY available_at, occurred_at, id LIMIT 20`, nil, "outbox_messages_ready_idx")
}

func assertPlanUsesIndex(t *testing.T, ctx context.Context, conn *pgx.Conn, query string, argument any, index string) {
	t.Helper()
	var rows pgx.Rows
	var err error
	if argument == nil {
		rows, err = conn.Query(ctx, `EXPLAIN (COSTS OFF) `+query)
	} else {
		rows, err = conn.Query(ctx, `EXPLAIN (COSTS OFF) `+query, argument)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), index) {
		t.Fatalf("access path did not use %s:\n%s", index, plan.String())
	}
}

func migrationPrefixFixture(t *testing.T, sources fs.FS, count int) fstest.MapFS {
	t.Helper()
	fixture := make(fstest.MapFS, count)
	for i := 1; i <= count; i++ {
		matches, err := fs.Glob(sources, fmt.Sprintf("%03d_*.sql", i))
		if err != nil || len(matches) != 1 {
			t.Fatalf("resolve prior migration %03d: matches=%v error=%v", i, matches, err)
		}
		contents, err := fs.ReadFile(sources, matches[0])
		if err != nil {
			t.Fatal(err)
		}
		fixture[matches[0]] = &fstest.MapFile{Data: contents}
	}
	return fixture
}

func migrateAndRequireVersion(t *testing.T, ctx context.Context, conn *pgx.Conn, sources fs.FS, want int32) {
	t.Helper()
	migrator, err := NewMigrator(ctx, conn, sources)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	version, err := migrator.Version(ctx)
	if err != nil || version != want {
		t.Fatalf("migration version = %d, %v; want %d", version, err, want)
	}
	if pending, err := migrator.HasPending(ctx); err != nil || pending {
		t.Fatalf("pending migrations = %t, %v", pending, err)
	}
}

func newMigrationTestDatabase(t *testing.T, databaseURL string) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	config, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	name := "data011_" + strings.ReplaceAll(mustMigrationID(t), "-", "")
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	config.Database = name
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		_, _ = admin.Exec(ctx, "DROP DATABASE "+identifier+" WITH (FORCE)")
		admin.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close(context.Background())
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+identifier+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	return conn
}

func assertTableExists(t *testing.T, ctx context.Context, conn *pgx.Conn, table string) {
	t.Helper()
	if !relationExists(t, ctx, conn, table) {
		t.Fatalf("table %q does not exist", table)
	}
}

func relationExists(t *testing.T, ctx context.Context, conn *pgx.Conn, table string) bool {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func mustMigrationID(t *testing.T) string {
	t.Helper()
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}
