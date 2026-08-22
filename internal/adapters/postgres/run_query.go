package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.RunQueryRepository = (*TenantRepository)(nil)

func (r *TenantRepository) GetRun(ctx context.Context, runID domain.ID) (ports.RunAccessRecord, error) {
	if err := r.valid(); err != nil {
		return ports.RunAccessRecord{}, err
	}
	if runID.IsZero() {
		return ports.RunAccessRecord{}, errors.New("run query requires an ID")
	}
	var record ports.RunAccessRecord
	var id, tenantID, agentID, versionID, requestedBy, digest, state, risk, owner string
	var parent *string
	err := r.db.QueryRow(ctx, `SELECT r.id::text,r.tenant_id::text,r.agent_id::text,r.agent_version_id::text,r.requested_by::text,
rv.agent_content_digest,r.parent_run_id::text,r.state,r.state_version,e.version,r.deadline_at,r.created_at,r.updated_at,a.risk_class,a.owner_principal_id::text
FROM runs r JOIN run_version_resolutions rv ON rv.tenant_id=r.tenant_id AND rv.run_id=r.id
JOIN resource_envelopes e ON e.tenant_id=r.tenant_id AND e.run_id=r.id
JOIN agents a ON a.tenant_id=r.tenant_id AND a.id=r.agent_id
WHERE r.tenant_id=$1 AND r.id=$2`, r.tenantID.String(), runID.String()).Scan(&id, &tenantID, &agentID, &versionID, &requestedBy, &digest, &parent, &state, &record.Run.StateVersion, &record.Run.EnvelopeVersion, &record.Run.DeadlineAt, &record.Run.CreatedAt, &record.Run.UpdatedAt, &risk, &owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.RunAccessRecord{}, domain.NewError(domain.CodeNotFound, "run not found")
	}
	if err != nil {
		return ports.RunAccessRecord{}, fmt.Errorf("query tenant run: %w", err)
	}
	values := []string{id, tenantID, agentID, versionID, requestedBy, owner}
	targets := []*domain.ID{&record.Run.ID, &record.Run.TenantID, &record.Run.AgentID, &record.Run.AgentVersionID, &record.Run.RequestedBy, &record.AgentOwnerID}
	for index, value := range values {
		parsed, parseErr := domain.ParseID(value)
		if parseErr != nil {
			return ports.RunAccessRecord{}, fmt.Errorf("decode run identifier: %w", parseErr)
		}
		*targets[index] = parsed
	}
	if parent != nil {
		parsed, parseErr := domain.ParseID(*parent)
		if parseErr != nil {
			return ports.RunAccessRecord{}, fmt.Errorf("decode parent run identifier: %w", parseErr)
		}
		record.Run.ParentRunID = &parsed
	}
	record.Run.VersionDigest, record.Run.State, record.AgentRiskClass = digest, domain.RunState(state), domain.AgentRiskClass(risk)
	record.Run.CreatedAt, record.Run.UpdatedAt = record.Run.CreatedAt.UTC(), record.Run.UpdatedAt.UTC()
	if record.Run.DeadlineAt != nil {
		deadline := record.Run.DeadlineAt.UTC()
		record.Run.DeadlineAt = &deadline
	}
	if err := record.Run.Validate(); err != nil {
		return ports.RunAccessRecord{}, fmt.Errorf("validate stored run projection (id=%s tenant=%s agent=%s version=%s requested_by=%s state=%s state_version=%d envelope_version=%d): %w", record.Run.ID, record.Run.TenantID, record.Run.AgentID, record.Run.AgentVersionID, record.Run.RequestedBy, record.Run.State, record.Run.StateVersion, record.Run.EnvelopeVersion, err)
	}
	if !record.AgentRiskClass.Valid() {
		return ports.RunAccessRecord{}, errors.New("validate stored run projection: invalid agent risk class")
	}
	return record, nil
}
