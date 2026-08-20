package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.AgentRegistry = (*TenantRepository)(nil)

const agentProjection = `id::text, tenant_id::text, name, description,
owner_principal_id::text, sponsor_principal_id::text, risk_class, status,
created_at, updated_at`

func (r *TenantRepository) PrincipalEligibility(ctx context.Context, principalID domain.ID) (ports.PrincipalEligibility, error) {
	if err := r.valid(); err != nil {
		return ports.PrincipalEligibility{}, err
	}
	if principalID.IsZero() {
		return ports.PrincipalEligibility{}, errors.New("principal eligibility requires an ID")
	}
	var disabledAt *time.Time
	err := r.db.QueryRow(ctx, `SELECT disabled_at FROM principals WHERE tenant_id = $1 AND id = $2`, r.tenantID.String(), principalID.String()).Scan(&disabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.PrincipalEligibility{}, nil
	}
	if err != nil {
		return ports.PrincipalEligibility{}, fmt.Errorf("query principal eligibility: %w", err)
	}
	return ports.PrincipalEligibility{Exists: true, Disabled: disabledAt != nil}, nil
}

func (r *TenantRepository) CreateAgent(ctx context.Context, agent domain.Agent) error {
	if err := r.validAgent(agent); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `INSERT INTO agents
(id, tenant_id, name, description, owner_principal_id, sponsor_principal_id, risk_class, status, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, agent.ID.String(), r.tenantID.String(), agent.Name, agent.Description, agent.OwnerPrincipalID.String(), agent.SponsorPrincipalID.String(), string(agent.RiskClass), string(agent.Status), agent.CreatedAt, agent.UpdatedAt)
	if err == nil {
		return nil
	}
	switch ClassifyError(err) {
	case ErrorUniqueViolation:
		return domain.WrapError(domain.CodeConflict, "agent identifier or name already exists", err)
	case ErrorForeignKeyViolation, ErrorCheckViolation:
		return domain.WrapError(domain.CodeInvalidArgument, "agent details are invalid", err)
	default:
		return fmt.Errorf("create tenant agent: %w", err)
	}
}

func (r *TenantRepository) UpdateAgent(ctx context.Context, agent domain.Agent, expectedUpdatedAt time.Time) error {
	if err := r.validAgent(agent); err != nil {
		return err
	}
	if expectedUpdatedAt.IsZero() {
		return errors.New("agent update requires an expected update time")
	}
	tag, err := r.db.Exec(ctx, `UPDATE agents SET name=$3, description=$4, owner_principal_id=$5,
sponsor_principal_id=$6, risk_class=$7, status=$8, updated_at=$9
WHERE tenant_id=$1 AND id=$2 AND updated_at=$10`, r.tenantID.String(), agent.ID.String(), agent.Name, agent.Description, agent.OwnerPrincipalID.String(), agent.SponsorPrincipalID.String(), string(agent.RiskClass), string(agent.Status), agent.UpdatedAt, expectedUpdatedAt)
	if err != nil {
		switch ClassifyError(err) {
		case ErrorUniqueViolation:
			return domain.WrapError(domain.CodeConflict, "agent name already exists", err)
		case ErrorForeignKeyViolation, ErrorCheckViolation:
			return domain.WrapError(domain.CodeInvalidArgument, "agent details are invalid", err)
		default:
			return fmt.Errorf("update tenant agent: %w", err)
		}
	}
	if tag.RowsAffected() != 1 {
		return domain.NewError(domain.CodeConflict, "agent was modified or does not exist")
	}
	return nil
}

func (r *TenantRepository) DescribeAgent(ctx context.Context, agentID domain.ID) (domain.Agent, error) {
	if err := r.valid(); err != nil {
		return domain.Agent{}, err
	}
	if agentID.IsZero() {
		return domain.Agent{}, errors.New("agent description requires an ID")
	}
	return scanAgent(r.db.QueryRow(ctx, `SELECT `+agentProjection+` FROM agents WHERE tenant_id=$1 AND id=$2`, r.tenantID.String(), agentID.String()))
}

func (r *TenantRepository) ListAgents(ctx context.Context, query ports.AgentListQuery) ([]domain.Agent, error) {
	if err := r.valid(); err != nil {
		return nil, err
	}
	if query.Limit < 1 || query.Limit > 200 {
		return nil, errors.New("agent list limit must be from 1 to 200")
	}
	var after any
	if !query.After.IsZero() {
		after = query.After.String()
	}
	rows, err := r.db.Query(ctx, `SELECT `+agentProjection+` FROM agents
WHERE tenant_id=$1 AND ($2::uuid IS NULL OR id > $2::uuid) ORDER BY id LIMIT $3`, r.tenantID.String(), after, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("list tenant agents: %w", err)
	}
	defer rows.Close()
	agents := make([]domain.Agent, 0, query.Limit)
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant agents: %w", err)
	}
	return agents, nil
}

type rowScanner interface{ Scan(...any) error }

func scanAgent(row rowScanner) (domain.Agent, error) {
	var agent domain.Agent
	var id, tenantID, ownerID, sponsorID, risk, status string
	if err := row.Scan(&id, &tenantID, &agent.Name, &agent.Description, &ownerID, &sponsorID, &risk, &status, &agent.CreatedAt, &agent.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Agent{}, domain.NewError(domain.CodeNotFound, "agent not found")
		}
		return domain.Agent{}, fmt.Errorf("scan tenant agent: %w", err)
	}
	var err error
	if agent.ID, err = domain.ParseID(id); err != nil {
		return domain.Agent{}, fmt.Errorf("decode agent ID: %w", err)
	}
	if agent.TenantID, err = domain.ParseID(tenantID); err != nil {
		return domain.Agent{}, fmt.Errorf("decode agent tenant ID: %w", err)
	}
	if agent.OwnerPrincipalID, err = domain.ParseID(ownerID); err != nil {
		return domain.Agent{}, fmt.Errorf("decode agent owner ID: %w", err)
	}
	if agent.SponsorPrincipalID, err = domain.ParseID(sponsorID); err != nil {
		return domain.Agent{}, fmt.Errorf("decode agent sponsor ID: %w", err)
	}
	agent.RiskClass, agent.Status = domain.AgentRiskClass(risk), domain.AgentStatus(status)
	// pgx may decode timestamptz using the process-local location. The database
	// value is an instant; normalize it at the adapter boundary before domain
	// validation so domain time remains explicitly UTC.
	agent.CreatedAt, agent.UpdatedAt = agent.CreatedAt.UTC(), agent.UpdatedAt.UTC()
	if err := agent.Validate(); err != nil {
		return domain.Agent{}, fmt.Errorf("validate stored agent: %w", err)
	}
	return agent, nil
}

func (r *TenantRepository) valid() error {
	if r == nil || r.db == nil || r.tenantID.IsZero() {
		return errors.New("postgres tenant repository is not initialized")
	}
	return nil
}

func (r *TenantRepository) validAgent(agent domain.Agent) error {
	if err := r.valid(); err != nil {
		return err
	}
	if agent.TenantID != r.tenantID {
		return errors.New("agent tenant does not match repository scope")
	}
	return agent.Validate()
}
