package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
	"time"
)

func (r *TenantRepository) Provision(ctx context.Context, b ports.BootstrapSpec, a ports.PolicyArtifact, digest string, at time.Time, fn func(ports.BootstrapRepository) error) (ports.BootstrapResult, error) {
	var result ports.BootstrapResult
	if b.TenantID != r.tenantID {
		return result, domain.NewError(domain.CodeForbidden, "bootstrap tenant mismatch")
	}
	e := r.withAdmissionTransaction(ctx, func(tx *TenantRepository) error {
		if _, e := tx.db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, b.TenantID.String()); e != nil {
			return e
		}
		var prior string
		var raw []byte
		e := tx.db.QueryRow(ctx, `SELECT request_digest,result FROM operator_bootstrap_receipts WHERE tenant_id=$1`, r.tenantID.String()).Scan(&prior, &raw)
		if e == nil {
			if prior != digest {
				return domain.NewError(domain.CodeConflict, "tenant already provisioned with another specification")
			}
			if b.RepairResourceCatalog {
				changed, err := tx.ensureBootstrapResources(ctx, at)
				if err != nil {
					return err
				}
				if changed {
					_, active, err := tx.CurrentPolicy(ctx, b.Channel)
					if err != nil {
						return err
					}
					decision, err := domain.NewID()
					if err != nil {
						return err
					}
					request, err := domain.NewID()
					if err != nil {
						return err
					}
					if err = tx.administrationEvidence(ctx, ports.AdministrationOperation{TenantID: b.TenantID, ActorID: b.Principals[0], DecisionID: decision, RequestID: request, Action: "operator.bootstrap.resources", Resource: b.TenantID.String(), PolicyDigest: active.Digest, PolicyVersion: active.Version, RequestHash: digest, At: at}); err != nil {
						return err
					}
				}
			}
			return json.Unmarshal(raw, &result)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		var exists bool
		if e = tx.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE id=$1)`, r.tenantID.String()).Scan(&exists); e != nil {
			return e
		}
		if exists {
			return domain.NewError(domain.CodeConflict, "bootstrap cannot replace an existing tenant")
		}
		if _, e = tx.db.Exec(ctx, `INSERT INTO tenants(id,slug,display_name,created_at,updated_at)VALUES($1,$2,$2,$3,$3)`, b.TenantID.String(), b.Slug, at); e != nil {
			return e
		}
		for _, p := range b.Principals {
			if _, e = tx.db.Exec(ctx, `INSERT INTO principals(id,tenant_id,external_issuer,external_subject,principal_type,created_at)VALUES($1,$2,$3,$4,'HUMAN',$5)`, p.String(), b.TenantID.String(), b.Issuer, p.String(), at); e != nil {
				return e
			}
		}
		if _, e = tx.ensureBootstrapResources(ctx, at); e != nil {
			return e
		}
		mappings, _ := json.Marshal(b.Mappings)
		connection, _ := json.Marshal(b.OPA)
		if _, e = tx.db.Exec(ctx, `INSERT INTO role_mapping_revisions(tenant_id,issuer,revision,mappings,created_by,created_at)VALUES($1,$2,1,$3,$4,$5)`, b.TenantID.String(), b.Issuer, mappings, b.Principals[0].String(), at); e != nil {
			return e
		}
		if _, e = tx.db.Exec(ctx, `INSERT INTO integration_revisions(tenant_id,integration,revision,connection,created_by,created_at)VALUES($1,'opa',1,$2,$3,$4)`, b.TenantID.String(), connection, b.Principals[0].String(), at); e != nil {
			return e
		}
		store := &PolicyStore{db: tx.db}
		if e = store.persistVerified(ctx, PolicyBundle{ID: a.ID, TenantID: b.TenantID, CreatedBy: b.Principals[0], Channel: b.Channel, Digest: a.Digest, ContractVersion: a.ContractVersion, ArtifactRevision: a.Revision, Bundle: a.Source, Signature: a.Signature.Value, SignerKeyID: a.Signature.KeyID, SignerKeyVersion: a.Signature.KeyVersion, SignatureAlgorithm: a.Signature.Algorithm, CreatedAt: at}); e != nil {
			return e
		}
		activation, e := domain.NewID()
		if e != nil {
			return e
		}
		decision, e := domain.NewID()
		if e != nil {
			return e
		}
		request, e := domain.NewID()
		if e != nil {
			return e
		}
		if _, e = tx.db.Exec(ctx, `INSERT INTO policy_activations(id,tenant_id,channel,policy_bundle_id,activation_version,actor_principal_id,policy_decision_id,reason_code,activated_at)VALUES($1,$2,$3,$4,1,$5,$6,'operator.bootstrap',$7)`, activation.String(), b.TenantID.String(), b.Channel, a.ID.String(), b.Principals[0].String(), decision.String(), at); e != nil {
			return e
		}
		if _, e = tx.db.Exec(ctx, `INSERT INTO tenant_security_epochs(tenant_id,policy_epoch,revocation_epoch)VALUES($1,1,0)`, b.TenantID.String()); e != nil {
			return e
		}
		if e = fn(tx); e != nil {
			return e
		}
		if e = tx.administrationEvidence(ctx, ports.AdministrationOperation{TenantID: b.TenantID, ActorID: b.Principals[0], DecisionID: decision, RequestID: request, Action: "operator.bootstrap", Resource: b.TenantID.String(), PolicyDigest: a.Digest, PolicyVersion: 1, RequestHash: digest, At: at}); e != nil {
			return e
		}
		vd, e := b.Manifest.ContentDigest()
		if e != nil {
			return e
		}
		result = ports.BootstrapResult{TenantID: b.TenantID, AgentID: b.AgentID, VersionDigest: vd, PolicyDigest: a.Digest}
		raw, e = json.Marshal(result)
		if e != nil {
			return e
		}
		_, e = tx.db.Exec(ctx, `INSERT INTO operator_bootstrap_receipts(tenant_id,request_digest,result,created_at)VALUES($1,$2,$3,$4)`, b.TenantID.String(), digest, raw, at)
		return e
	})
	return result, e
}

// Definitions enable typed accounting; they allocate no resource authority.
// Actual grants still intersect policy, approved manifest and deployment limits.
func (r *TenantRepository) ensureBootstrapResources(ctx context.Context, at time.Time) (bool, error) {
	changed := false
	for _, name := range []string{"budget_usd_microunits", "llm_tokens", "tool_calls", "tool_calls_per_minute", "active_children", "total_children", "delegation_depth"} {
		class, aggregation, unit := domain.ResourceConsumable, domain.ResourceSum, name
		switch name {
		case "tool_calls_per_minute", "active_children", "total_children", "delegation_depth":
			class, aggregation = domain.ResourceStructural, domain.ResourceMaximum
		}
		id, e := domain.NewID()
		if e != nil {
			return false, e
		}
		d := domain.ResourceDimension{ID: id, TenantID: r.tenantID, Name: name, Class: class, Aggregation: aggregation, Unit: unit, Scale: 0, Minimum: 0, Maximum: 1 << 53}
		if e = d.Validate(); e != nil {
			return false, e
		}
		tag, e := r.db.Exec(ctx, `INSERT INTO resource_dimensions(id,tenant_id,name,class,unit,scale,minimum_value,maximum_value,aggregation,created_at) VALUES($1,$2,$3,$4,$5,0,0,$6,$7,$8) ON CONFLICT(tenant_id,name) DO NOTHING`, id.String(), r.tenantID.String(), name, string(class), unit, d.Maximum, string(aggregation), at)
		if e != nil {
			return false, e
		}
		if tag.RowsAffected() > 0 {
			changed = true
		} else {
			var matches bool
			e = r.db.QueryRow(ctx, `SELECT class=$3 AND unit=$4 AND scale=0 AND minimum_value=0 AND maximum_value=$5 AND aggregation=$6 FROM resource_dimensions WHERE tenant_id=$1 AND name=$2`, r.tenantID.String(), name, string(class), unit, d.Maximum, string(aggregation)).Scan(&matches)
			if e != nil {
				return false, e
			}
			if !matches {
				return false, domain.NewError(domain.CodeConflict, "existing resource catalog differs; operator review required")
			}
		}
	}
	return changed, nil
}
