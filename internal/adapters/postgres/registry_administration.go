package postgres

import (
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

func (r *TenantRepository) RegistryTransaction(ctx context.Context, op ports.AdministrationOperation, fn func(ports.RegistryRepository) (any, error)) (json.RawMessage, error) {
	return r.administrationTransaction(ctx, op, func(tx *TenantRepository) (any, error) { return fn(tx) })
}
func (r *TenantRepository) ListRunIDs(ctx context.Context, after domain.ID, limit int) ([]domain.ID, error) {
	if limit < 1 || limit > 101 {
		return nil, domain.NewError(domain.CodeInvalidArgument, "invalid run page limit")
	}
	rows, e := r.db.Query(ctx, `SELECT id::text FROM runs WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT $3`, r.tenantID.String(), after.String(), limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.ID{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		id, e := domain.ParseID(raw)
		if e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
