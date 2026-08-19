package postgres

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	tern "github.com/jackc/tern/v2/migrate"
)

const migrationTable = "public.thinkpixelag_schema_version"
const migrationChecksumManifest = "checksums.sha256"

// Migrator is the narrow, forward-only schema migration surface used by the
// explicit migration command and migration tests.
type Migrator interface {
	Up(context.Context) error
	Version(context.Context) (int32, error)
	HasPending(context.Context) (bool, error)
}

type ternMigrator struct{ migrator *tern.Migrator }

// NewMigrator validates and loads migration sources. Tern creates its version
// table through conn; the caller owns and must close the connection.
func NewMigrator(ctx context.Context, conn *pgx.Conn, sources fs.FS) (Migrator, error) {
	if conn == nil {
		return nil, errors.New("postgres migrator requires a connection")
	}
	if sources == nil {
		return nil, errors.New("postgres migrator requires migration sources")
	}
	if err := validateMigrationSources(sources); err != nil {
		return nil, fmt.Errorf("validate postgres migration sources: %w", err)
	}
	migrator, err := tern.NewMigrator(ctx, conn, migrationTable)
	if err != nil {
		return nil, fmt.Errorf("initialize postgres migrations: %w", err)
	}
	if err := migrator.LoadMigrations(sources); err != nil {
		return nil, fmt.Errorf("load postgres migrations: %w", err)
	}
	return &ternMigrator{migrator: migrator}, nil
}

func validateMigrationSources(sources fs.FS) error {
	if sources == nil {
		return errors.New("migration sources are required")
	}
	entries, err := fs.ReadDir(sources, ".")
	if err != nil {
		return err
	}
	paths, err := tern.FindMigrations(sources)
	if err != nil {
		return err
	}
	valid := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		valid[path] = struct{}{}
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(path.Ext(entry.Name()), ".sql") {
			continue
		}
		if _, ok := valid[entry.Name()]; !ok {
			return fmt.Errorf("invalid migration filename %q", entry.Name())
		}
	}
	if _, err := fs.Stat(sources, migrationChecksumManifest); err == nil {
		if err := validateMigrationChecksums(sources, valid); err != nil {
			return err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func validateMigrationChecksums(sources fs.FS, migrations map[string]struct{}) error {
	manifest, err := sources.Open(migrationChecksumManifest)
	if err != nil {
		return err
	}
	defer manifest.Close()

	remaining := make(map[string]struct{}, len(migrations))
	for name := range migrations {
		remaining[name] = struct{}{}
	}
	scanner := bufio.NewScanner(manifest)
	line := 0
	for scanner.Scan() {
		line++
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) != 2 {
			return fmt.Errorf("invalid migration checksum manifest line %d", line)
		}
		digest, name := fields[0], strings.TrimPrefix(fields[1], "*")
		if len(digest) != sha256.Size*2 {
			return fmt.Errorf("invalid checksum for migration %q", name)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return fmt.Errorf("invalid checksum for migration %q", name)
		}
		if _, ok := remaining[name]; !ok {
			return fmt.Errorf("checksum manifest contains unknown or duplicate migration %q", name)
		}
		contents, err := fs.ReadFile(sources, name)
		if err != nil {
			return err
		}
		actual := fmt.Sprintf("%x", sha256.Sum256(contents))
		if !strings.EqualFold(actual, digest) {
			return fmt.Errorf("released migration %q checksum mismatch", name)
		}
		delete(remaining, name)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(remaining) != 0 {
		return errors.New("migration checksum manifest does not cover every migration")
	}
	return nil
}

func (m *ternMigrator) Up(ctx context.Context) error {
	if err := m.migrator.Migrate(ctx); err != nil {
		return fmt.Errorf("apply postgres migrations: %w", err)
	}
	return nil
}

func (m *ternMigrator) Version(ctx context.Context) (int32, error) {
	version, err := m.migrator.GetCurrentVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read postgres migration version: %w", err)
	}
	return version, nil
}

func (m *ternMigrator) HasPending(ctx context.Context) (bool, error) {
	version, err := m.Version(ctx)
	if err != nil {
		return false, err
	}
	return int(version) < len(m.migrator.Migrations), nil
}

var _ Migrator = (*ternMigrator)(nil)
