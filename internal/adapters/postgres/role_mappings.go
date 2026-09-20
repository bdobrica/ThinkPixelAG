package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

func (r *TenantRepository) RoleMappings(ctx context.Context, issuer string) (ports.RoleMappings, error) {
	m := ports.RoleMappings{Mode: "api", Issuer: issuer}
	var raw []byte
	err := r.db.QueryRow(ctx, `SELECT revision,mappings,created_at FROM role_mapping_revisions WHERE tenant_id=$1 AND issuer=$2 ORDER BY revision DESC LIMIT 1`, r.tenantID.String(), issuer).Scan(&m.Revision, &raw, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, domain.NewError(domain.CodeUnavailable, "authoritative role mappings unavailable")
	}
	if err == nil {
		err = json.Unmarshal(raw, &m.Mappings)
	}
	m.UpdatedAt = m.UpdatedAt.UTC()
	return m, err
}
func (r *TenantRepository) SaveRoleMappings(ctx context.Context, op ports.AdministrationOperation, m ports.RoleMappings, approvalRef string) (ports.RoleMappings, error) {
	raw, err := r.administrationTransaction(ctx, op, func(tx *TenantRepository) (any, error) {
		old, err := tx.RoleMappings(ctx, m.Issuer)
		if err != nil {
			return nil, err
		}
		if old.Revision != m.Revision {
			return nil, domain.NewError(domain.CodeConflict, "role mappings changed")
		}
		if err := domain.ValidateRoleMappings(m.Mappings); err != nil {
			return nil, err
		}
		if domain.RoleMappingExpansion(old.Mappings, m.Mappings) {
			id, e := domain.ParseID(approvalRef)
			if e != nil {
				return nil, domain.NewError(domain.CodeForbidden, "administrative expansion requires independent approval")
			}
			a, e := tx.GovernanceApproval(ctx, id)
			if e != nil {
				return nil, e
			}
			if a.Action != domain.ApprovalEmergencyExpansion || a.RequesterPrincipalID != op.ActorID {
				return nil, domain.NewError(domain.CodeForbidden, "expansion approval has wrong action or requester")
			}
			if _, e = tx.ConsumeGovernanceApproval(ctx, id, domain.RoleMappingDigest(r.tenantID, m.Issuer, m.Revision, m.Mappings), op.At); e != nil {
				return nil, e
			}
		} else if approvalRef != "not-required" {
			return nil, domain.NewError(domain.CodeInvalidArgument, "non-expanding edit uses not-required")
		}
		payload, e := json.Marshal(m.Mappings)
		if e != nil {
			return nil, e
		}
		m.Revision++
		m.Mode = "api"
		m.UpdatedAt = op.At
		_, e = tx.db.Exec(ctx, `INSERT INTO role_mapping_revisions(tenant_id,issuer,revision,mappings,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6)`, r.tenantID.String(), m.Issuer, m.Revision, payload, op.ActorID.String(), op.At)
		return m, e
	})
	var m2 ports.RoleMappings
	if err == nil {
		err = json.Unmarshal(raw, &m2)
	}
	return m2, err
}
