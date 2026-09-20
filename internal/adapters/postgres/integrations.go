package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

func (r *TenantRepository) OPAIntegration(ctx context.Context) (ports.IntegrationSettings, error) {
	m := ports.IntegrationSettings{ID: "opa", Mode: "api"}
	var raw []byte
	e := r.db.QueryRow(ctx, `SELECT revision,connection FROM integration_revisions WHERE tenant_id=$1 AND integration='opa' ORDER BY revision DESC LIMIT 1`, r.tenantID.String()).Scan(&m.Revision, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return m, domain.NewError(domain.CodeUnavailable, "authoritative OPA configuration unavailable")
	}
	if e == nil {
		e = json.Unmarshal(raw, &m.Connection)
	}
	return m, e
}
func (r *TenantRepository) SaveOPAIntegration(ctx context.Context, op ports.AdministrationOperation, expected int64, c ports.OPAConnection) (ports.IntegrationSettings, error) {
	raw, e := r.administrationTransaction(ctx, op, func(tx *TenantRepository) (any, error) {
		current, e := tx.OPAIntegration(ctx)
		if e != nil {
			return nil, e
		}
		if current.Revision != expected {
			return nil, domain.NewError(domain.CodeConflict, "integration configuration changed")
		}
		value, e := json.Marshal(c)
		if e != nil {
			return nil, e
		}
		_, e = tx.db.Exec(ctx, `INSERT INTO integration_revisions(tenant_id,integration,revision,connection,created_by,created_at) VALUES($1,'opa',$2,$3,$4,$5)`, r.tenantID.String(), expected+1, value, op.ActorID.String(), op.At)
		return ports.IntegrationSettings{ID: "opa", Mode: "api", Revision: expected + 1, Connection: c}, e
	})
	var m ports.IntegrationSettings
	if e == nil {
		e = json.Unmarshal(raw, &m)
	}
	return m, e
}
