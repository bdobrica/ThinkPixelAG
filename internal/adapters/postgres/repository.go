package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ErrRecordNotFound deliberately does not distinguish a missing record from a
// record owned by another tenant. That property is required at every caller
// boundary to avoid identifier enumeration across tenants.
var ErrRecordNotFound = errors.New("postgres record not found")

// RecordKind is the closed set of tenant-addressable Phase 2 records. Table
// names never come from callers; repository SQL remains parameterized and
// reviewable despite the shared identity lookup implementation.
type RecordKind string

const (
	RecordPrincipal           RecordKind = "principal"
	RecordAgent               RecordKind = "agent"
	RecordAgentVersion        RecordKind = "agent_version"
	RecordAgentCapability     RecordKind = "agent_capability"
	RecordAgentApproval       RecordKind = "agent_approval"
	RecordPolicyBundle        RecordKind = "policy_bundle"
	RecordPolicyActivation    RecordKind = "policy_activation"
	RecordRun                 RecordKind = "run"
	RecordRunResolution       RecordKind = "run_resolution"
	RecordRunSignal           RecordKind = "run_signal"
	RecordRunEvent            RecordKind = "run_event"
	RecordResourceDimension   RecordKind = "resource_dimension"
	RecordResourceEnvelope    RecordKind = "resource_envelope"
	RecordResourceReservation RecordKind = "resource_reservation"
	RecordTrustedUsage        RecordKind = "trusted_usage"
	RecordResourceSettlement  RecordKind = "resource_settlement"
	RecordRevocation          RecordKind = "revocation"
	RecordRevocationChange    RecordKind = "revocation_change"
	RecordIdempotency         RecordKind = "idempotency"
	RecordAuditEvent          RecordKind = "audit_event"
	RecordOutboxMessage       RecordKind = "outbox_message"
)

type recordQuery struct {
	table string
	id    string
}

var tenantRecordQueries = map[RecordKind]recordQuery{
	RecordPrincipal:           {"principals", "id"},
	RecordAgent:               {"agents", "id"},
	RecordAgentVersion:        {"agent_versions", "id"},
	RecordAgentCapability:     {"agent_capabilities", "id"},
	RecordAgentApproval:       {"agent_version_approvals", "id"},
	RecordPolicyBundle:        {"policy_bundles", "id"},
	RecordPolicyActivation:    {"policy_activations", "id"},
	RecordRun:                 {"runs", "id"},
	RecordRunResolution:       {"run_version_resolutions", "run_id"},
	RecordRunSignal:           {"run_signals", "id"},
	RecordRunEvent:            {"run_events", "id"},
	RecordResourceDimension:   {"resource_dimensions", "id"},
	RecordResourceEnvelope:    {"resource_envelopes", "id"},
	RecordResourceReservation: {"resource_reservations", "id"},
	RecordTrustedUsage:        {"trusted_usage_entries", "id"},
	RecordResourceSettlement:  {"resource_settlements", "id"},
	RecordRevocation:          {"revocations", "id"},
	RecordRevocationChange:    {"revocation_changes", "id"},
	RecordIdempotency:         {"idempotency_records", "id"},
	RecordAuditEvent:          {"audit_events", "id"},
	RecordOutboxMessage:       {"outbox_messages", "id"},
}

// RecordIdentity is the minimum common projection shared by tenant-owned
// aggregate records. Aggregate repositories add typed projections and
// mutations as their owning phases are implemented.
type RecordIdentity struct {
	ID       domain.ID
	TenantID domain.ID
}

// Repositories owns the database handle but no transaction lifecycle.
type Repositories struct{ db DBTX }

func NewRepositories(db DBTX) (*Repositories, error) {
	if db == nil {
		return nil, errors.New("postgres repositories require a database handle")
	}
	return &Repositories{db: db}, nil
}

// WithDB rebinds repositories to a transaction-bound handle. It is intended
// for use inside Transactor callbacks and cannot commit or roll back that
// transaction.
func (r *Repositories) WithDB(db DBTX) (*Repositories, error) {
	return NewRepositories(db)
}

// ForTenant creates a repository whose every operation includes the bound
// tenant as the first SQL predicate.
func (r *Repositories) ForTenant(tenantID domain.ID) (*TenantRepository, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("postgres repositories are not initialized")
	}
	if tenantID.IsZero() {
		return nil, errors.New("postgres tenant repository requires a tenant ID")
	}
	return &TenantRepository{db: r.db, tenantID: tenantID}, nil
}

type TenantRepository struct {
	db       DBTX
	tenantID domain.ID
}

// FindIdentity returns a same-tenant record identity. Both a missing ID and an
// ID owned by a different tenant produce ErrRecordNotFound.
func (r *TenantRepository) FindIdentity(ctx context.Context, kind RecordKind, id domain.ID) (RecordIdentity, error) {
	if r == nil || r.db == nil || r.tenantID.IsZero() {
		return RecordIdentity{}, errors.New("postgres tenant repository is not initialized")
	}
	if id.IsZero() {
		return RecordIdentity{}, errors.New("postgres record lookup requires an ID")
	}
	query, ok := tenantRecordQueries[kind]
	if !ok {
		return RecordIdentity{}, fmt.Errorf("unsupported postgres record kind %q", kind)
	}

	statement := fmt.Sprintf(
		"SELECT %s::text, tenant_id::text FROM %s WHERE tenant_id = $1 AND %s = $2",
		query.id, query.table, query.id,
	)
	var recordID, tenantID string
	if err := r.db.QueryRow(ctx, statement, r.tenantID.String(), id.String()).Scan(&recordID, &tenantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RecordIdentity{}, ErrRecordNotFound
		}
		return RecordIdentity{}, fmt.Errorf("find tenant %s: %w", kind, err)
	}
	parsedRecordID, err := domain.ParseID(recordID)
	if err != nil {
		return RecordIdentity{}, fmt.Errorf("decode tenant %s ID: %w", kind, err)
	}
	parsedTenantID, err := domain.ParseID(tenantID)
	if err != nil {
		return RecordIdentity{}, fmt.Errorf("decode tenant %s tenant ID: %w", kind, err)
	}
	return RecordIdentity{ID: parsedRecordID, TenantID: parsedTenantID}, nil
}
