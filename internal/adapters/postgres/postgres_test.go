package postgres

import (
	"context"
	"testing"
	"testing/fstest"
)

func TestConstructorsRejectMissingDependencies(t *testing.T) {
	t.Parallel()

	if _, err := NewTransactor(nil); err == nil {
		t.Fatal("NewTransactor(nil) succeeded")
	}
	if _, err := NewMigrator(context.Background(), nil, fstest.MapFS{}); err == nil {
		t.Fatal("NewMigrator(nil, fs) succeeded")
	}
	if err := validateMigrationSources(nil); err == nil {
		t.Fatal("validateMigrationSources(nil) succeeded")
	}
}

func TestMigratorRejectsInvalidSources(t *testing.T) {
	t.Parallel()

	err := validateMigrationSources(fstest.MapFS{
		"not-a-migration.sql": {Data: []byte("SELECT 1;")},
	})
	if err == nil {
		t.Fatal("validateMigrationSources accepted an invalid migration filename")
	}
}

func TestMigrationSourcesMustBeContiguous(t *testing.T) {
	t.Parallel()

	err := validateMigrationSources(fstest.MapFS{
		"001_first.sql": {Data: []byte("SELECT 1;")},
		"003_third.sql": {Data: []byte("SELECT 3;")},
	})
	if err == nil {
		t.Fatal("validateMigrationSources accepted a sequence gap")
	}
}

func TestMigrationSourcesAcceptContiguousSequence(t *testing.T) {
	t.Parallel()

	err := validateMigrationSources(fstest.MapFS{
		"001_first.sql":  {Data: []byte("SELECT 1;")},
		"002_second.sql": {Data: []byte("SELECT 2;")},
		"README.md":      {Data: []byte("documentation")},
	})
	if err != nil {
		t.Fatalf("validateMigrationSources returned %v", err)
	}
}
