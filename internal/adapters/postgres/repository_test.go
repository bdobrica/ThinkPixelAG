package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRepositoriesRejectMissingScope(t *testing.T) {
	t.Parallel()
	if _, err := NewRepositories(nil); err == nil {
		t.Fatal("NewRepositories(nil) succeeded")
	}
	repositories, err := NewRepositories(&recordDB{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.ForTenant(domain.ID{}); err == nil {
		t.Fatal("ForTenant accepted a zero tenant ID")
	}
}

func TestTenantRecordKindsAreClosedAndScoped(t *testing.T) {
	t.Parallel()
	tenantID := mustNewRepositoryID(t)
	recordID := mustNewRepositoryID(t)
	db := &recordDB{row: recordRow{values: []string{recordID.String(), tenantID.String()}}}
	repositories, err := NewRepositories(db)
	if err != nil {
		t.Fatal(err)
	}
	tenantRepository, err := repositories.ForTenant(tenantID)
	if err != nil {
		t.Fatal(err)
	}

	for kind := range tenantRecordQueries {
		got, err := tenantRepository.FindIdentity(context.Background(), kind, recordID)
		if err != nil {
			t.Fatalf("FindIdentity(%q): %v", kind, err)
		}
		if got.ID != recordID || got.TenantID != tenantID {
			t.Fatalf("FindIdentity(%q) = %+v", kind, got)
		}
		if !strings.Contains(db.query, "WHERE tenant_id = $1 AND ") {
			t.Fatalf("FindIdentity(%q) query is not tenant-first: %s", kind, db.query)
		}
		if len(db.args) != 2 || db.args[0] != tenantID.String() || db.args[1] != recordID.String() {
			t.Fatalf("FindIdentity(%q) args = %#v", kind, db.args)
		}
	}
	if _, err := tenantRepository.FindIdentity(context.Background(), RecordKind("callers-cannot-select-tables"), recordID); err == nil {
		t.Fatal("FindIdentity accepted an unknown record kind")
	}
}

func TestTenantRepositoryHidesMissingAndCrossTenantRecords(t *testing.T) {
	t.Parallel()
	tenantID := mustNewRepositoryID(t)
	repositories, err := NewRepositories(&recordDB{row: recordRow{err: pgx.ErrNoRows}})
	if err != nil {
		t.Fatal(err)
	}
	tenantRepository, err := repositories.ForTenant(tenantID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tenantRepository.FindIdentity(context.Background(), RecordPrincipal, mustNewRepositoryID(t))
	if !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("FindIdentity error = %v, want ErrRecordNotFound", err)
	}
}

func mustNewRepositoryID(t *testing.T) domain.ID {
	t.Helper()
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type recordDB struct {
	query string
	args  []any
	row   recordRow
}

func (db *recordDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (db *recordDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (db *recordDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	db.query, db.args = query, args
	return db.row
}

type recordRow struct {
	values []string
	err    error
}

func (r recordRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for index, value := range r.values {
		pointer, ok := dest[index].(*string)
		if !ok {
			return errors.New("recordRow requires string destinations")
		}
		*pointer = value
	}
	return nil
}
