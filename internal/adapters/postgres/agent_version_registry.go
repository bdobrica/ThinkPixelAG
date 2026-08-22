package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.AgentVersionRegistry = (*TenantRepository)(nil)

func (r *TenantRepository) RegisterAgentVersion(ctx context.Context, version domain.AgentVersion, capabilities []domain.AgentCapability) error {
	if err := r.valid(); err != nil {
		return err
	}
	if version.TenantID != r.tenantID {
		return errors.New("agent version tenant does not match repository scope")
	}
	if err := version.Validate(); err != nil {
		return err
	}
	canonical, err := version.Manifest.CanonicalJSON()
	if err != nil {
		return err
	}
	type capabilityRow struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Identifier string `json:"identifier"`
	}
	rows := make([]capabilityRow, len(capabilities))
	capabilityIDs := make([]domain.ID, len(capabilities))
	for index, capability := range capabilities {
		if capability.ID.IsZero() || capability.TenantID != version.TenantID || capability.AgentID != version.AgentID || capability.AgentVersionID != version.ID || capability.CreatedAt != version.CreatedAt {
			return errors.New("agent capability does not match its version")
		}
		capabilityIDs[index] = capability.ID
		rows[index] = capabilityRow{capability.ID.String(), string(capability.Type), capability.Identifier}
	}
	expected, err := version.Capabilities(capabilityIDs)
	if err != nil {
		return domain.WrapError(domain.CodeInvalidArgument, "agent capabilities are invalid", err)
	}
	for index := range capabilities {
		if capabilities[index] != expected[index] {
			return domain.NewError(domain.CodeInvalidArgument, "agent capabilities do not match canonical manifest")
		}
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("encode agent capabilities: %w", err)
	}
	var inserted int
	err = r.db.QueryRow(ctx, `WITH inserted_version AS (
  INSERT INTO agent_versions(id,tenant_id,agent_id,content_digest,image_digest,manifest_schema_version,manifest,created_by,created_at)
  SELECT $1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9
  FROM agents WHERE tenant_id=$2 AND id=$3 AND status <> 'RETIRED'
  RETURNING id
), inserted_capabilities AS (
  INSERT INTO agent_capabilities(id,tenant_id,agent_id,agent_version_id,capability_type,capability_identifier,constraints,created_at)
  SELECT value.id::uuid,$2,$3,$1,value.type,value.identifier,'{}'::jsonb,$9
  FROM inserted_version, jsonb_to_recordset($10::jsonb) AS value(id text,type text,identifier text)
  RETURNING id
)
SELECT count(*) FROM inserted_version`, version.ID.String(), r.tenantID.String(), version.AgentID.String(), version.ContentDigest, version.ImageDigest, domain.AgentManifestSchemaVersion, string(canonical), version.CreatedBy.String(), version.CreatedAt, string(payload)).Scan(&inserted)
	if err == nil && inserted == 0 {
		return domain.NewError(domain.CodeNotFound, "active agent not found")
	}
	if err == nil {
		return nil
	}
	switch ClassifyError(err) {
	case ErrorUniqueViolation:
		return domain.WrapError(domain.CodeConflict, "agent version digest or identifier already exists", err)
	case ErrorForeignKeyViolation, ErrorCheckViolation:
		return domain.WrapError(domain.CodeInvalidArgument, "agent version references are invalid", err)
	default:
		return fmt.Errorf("register tenant agent version: %w", err)
	}
}

func (r *TenantRepository) DescribeAgentVersion(ctx context.Context, agentID domain.ID, digest string) (domain.AgentVersion, []domain.AgentCapability, error) {
	if err := r.valid(); err != nil {
		return domain.AgentVersion{}, nil, err
	}
	if agentID.IsZero() {
		return domain.AgentVersion{}, nil, errors.New("agent version description requires an agent ID")
	}
	var version domain.AgentVersion
	var id, tenantID, storedAgentID, createdBy string
	var manifestJSON []byte
	err := r.db.QueryRow(ctx, `SELECT id::text,tenant_id::text,agent_id::text,content_digest,image_digest,manifest,created_by::text,created_at
FROM agent_versions WHERE tenant_id=$1 AND agent_id=$2 AND content_digest=$3`, r.tenantID.String(), agentID.String(), digest).Scan(&id, &tenantID, &storedAgentID, &version.ContentDigest, &version.ImageDigest, &manifestJSON, &createdBy, &version.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AgentVersion{}, nil, domain.NewError(domain.CodeNotFound, "agent version not found")
	}
	if err != nil {
		return domain.AgentVersion{}, nil, fmt.Errorf("describe tenant agent version: %w", err)
	}
	if version.ID, err = domain.ParseID(id); err != nil {
		return domain.AgentVersion{}, nil, fmt.Errorf("decode agent version ID: %w", err)
	}
	if version.TenantID, err = domain.ParseID(tenantID); err != nil {
		return domain.AgentVersion{}, nil, fmt.Errorf("decode agent version tenant: %w", err)
	}
	if version.AgentID, err = domain.ParseID(storedAgentID); err != nil {
		return domain.AgentVersion{}, nil, fmt.Errorf("decode agent version agent: %w", err)
	}
	if version.CreatedBy, err = domain.ParseID(createdBy); err != nil {
		return domain.AgentVersion{}, nil, fmt.Errorf("decode agent version creator: %w", err)
	}
	version.CreatedAt = version.CreatedAt.UTC()
	if version.Manifest, err = domain.ParseAgentManifest(manifestJSON); err != nil {
		return domain.AgentVersion{}, nil, fmt.Errorf("decode stored agent manifest: %w", err)
	}
	if err := version.Validate(); err != nil {
		return domain.AgentVersion{}, nil, fmt.Errorf("validate stored agent version: %w", err)
	}

	rows, err := r.db.Query(ctx, `SELECT id::text,capability_type,capability_identifier,created_at FROM agent_capabilities
WHERE tenant_id=$1 AND agent_id=$2 AND agent_version_id=$3 ORDER BY capability_type,capability_identifier`, r.tenantID.String(), agentID.String(), version.ID.String())
	if err != nil {
		return domain.AgentVersion{}, nil, fmt.Errorf("list agent version capabilities: %w", err)
	}
	defer rows.Close()
	capabilities := make([]domain.AgentCapability, 0)
	for rows.Next() {
		var capability domain.AgentCapability
		var capabilityID, capabilityType string
		if err := rows.Scan(&capabilityID, &capabilityType, &capability.Identifier, &capability.CreatedAt); err != nil {
			return domain.AgentVersion{}, nil, fmt.Errorf("scan agent capability: %w", err)
		}
		if capability.ID, err = domain.ParseID(capabilityID); err != nil {
			return domain.AgentVersion{}, nil, fmt.Errorf("decode agent capability ID: %w", err)
		}
		capability.TenantID, capability.AgentID, capability.AgentVersionID = version.TenantID, version.AgentID, version.ID
		capability.Type, capability.CreatedAt = domain.AgentCapabilityType(capabilityType), capability.CreatedAt.UTC()
		capabilities = append(capabilities, capability)
	}
	if err := rows.Err(); err != nil {
		return domain.AgentVersion{}, nil, fmt.Errorf("iterate agent capabilities: %w", err)
	}
	if err := validateStoredCapabilities(version, capabilities); err != nil {
		return domain.AgentVersion{}, nil, err
	}
	return version, capabilities, nil
}

func validateStoredCapabilities(version domain.AgentVersion, capabilities []domain.AgentCapability) error {
	actual := make(map[domain.AgentCapabilityType][]string)
	for _, capability := range capabilities {
		actual[capability.Type] = append(actual[capability.Type], capability.Identifier)
	}
	for _, expected := range []struct {
		typeName domain.AgentCapabilityType
		values   []string
	}{{domain.CapabilityModel, version.Manifest.Models}, {domain.CapabilityTool, version.Manifest.Tools}, {domain.CapabilitySkill, version.Manifest.Skills}, {domain.CapabilitySubagent, version.Manifest.Subagents}} {
		values := actual[expected.typeName]
		if len(values) != len(expected.values) {
			return errors.New("stored agent capabilities do not match immutable manifest")
		}
		for index := range values {
			if values[index] != expected.values[index] {
				return errors.New("stored agent capabilities do not match immutable manifest")
			}
		}
		delete(actual, expected.typeName)
	}
	if len(actual) != 0 {
		return errors.New("stored agent capabilities contain an unknown type")
	}
	return nil
}
