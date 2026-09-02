package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

// LoadRuntimeSecurityState returns every active, currently valid policy and
// the matching authoritative revocation head. An active invalid policy makes
// the whole refresh fail closed instead of silently disappearing from health.
func (r *Repositories) LoadRuntimeSecurityState(ctx context.Context, observedAt time.Time) ([]ports.RuntimeSecurityState, error) {
	if r == nil || r.db == nil || observedAt.IsZero() {
		return nil, fmt.Errorf("runtime security state requires repositories and observation time")
	}
	rows, err := r.db.Query(ctx, `SELECT pa.tenant_id::text,pa.channel,pa.policy_bundle_id::text,pb.content_digest,pa.activation_version,pa.activated_at,pb.validation_status,pb.contract_version,pb.valid_from,pb.valid_until,
COALESCE((SELECT max(sequence) FROM revocation_log WHERE tenant_id=pa.tenant_id OR tenant_id IS NULL),0),
(SELECT security_epoch FROM security_epochs WHERE singleton),
COALESCE((SELECT policy_epoch FROM tenant_security_epochs WHERE tenant_id=pa.tenant_id),0),
COALESCE((SELECT revocation_epoch FROM tenant_security_epochs WHERE tenant_id=pa.tenant_id),0),
COALESCE((SELECT max(revocation_epoch) FROM agent_security_epochs WHERE tenant_id=pa.tenant_id),0)
FROM policy_activations pa JOIN policy_bundles pb ON pb.tenant_id=pa.tenant_id AND pb.id=pa.policy_bundle_id
WHERE pa.deactivated_at IS NULL ORDER BY pa.tenant_id,pa.channel`)
	if err != nil {
		return nil, fmt.Errorf("load active policies: %w", err)
	}
	defer rows.Close()
	var out []ports.RuntimeSecurityState
	for rows.Next() {
		var tenantText, status, contractVersion string
		var validFrom, validUntil *time.Time
		var state ports.RuntimeSecurityState
		if err := rows.Scan(&tenantText, &state.Policy.Channel, &state.Policy.BundleID, &state.Policy.Digest, &state.Policy.Version, &state.Policy.ActivatedAt, &status, &contractVersion, &validFrom, &validUntil, &state.Sequence, &state.Epochs.Security, &state.Epochs.TenantPolicy, &state.Epochs.TenantRevocation, &state.Epochs.AgentRevocation); err != nil {
			return nil, fmt.Errorf("scan active policy: %w", err)
		}
		_, err := domain.ParseID(tenantText)
		if err != nil || (status != "VALIDATED" && status != "APPROVED") || contractVersion != policy.ContractVersion || validFrom != nil && observedAt.Before(*validFrom) || validUntil != nil && !observedAt.Before(*validUntil) {
			return nil, fmt.Errorf("active policy is not valid for serving")
		}
		state.Policy.TenantID, state.Policy.RefreshedAt = tenantText, observedAt
		out = append(out, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load active policies: %w", err)
	}
	return out, nil
}
