package postgres

import (
	"context"
	"strings"
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

func TestMigrationChecksumCompatibility(t *testing.T) {
	t.Parallel()

	valid := fstest.MapFS{
		"001_first.sql":    {Data: []byte("SELECT 1;")},
		"checksums.sha256": {Data: []byte("17db4fd369edb9244b9f91d9aeed145c3d04ad8ba6e95d06247f07a63527d11a  001_first.sql\n")},
	}
	if err := validateMigrationSources(valid); err != nil {
		t.Fatalf("valid checksum manifest rejected: %v", err)
	}
	valid["001_first.sql"] = &fstest.MapFile{Data: []byte("SELECT 2;")}
	if err := validateMigrationSources(valid); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("changed released migration error = %v", err)
	}
}
