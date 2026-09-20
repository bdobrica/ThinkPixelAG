package ports

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"time"
)

// BootstrapSpec is a deployment-reviewed initial snapshot, never an HTTP body.
type BootstrapSpec struct {
	TenantID   domain.ID            `json:"tenant_id"`
	Slug       string               `json:"slug"`
	Issuer     string               `json:"issuer"`
	Principals []domain.ID          `json:"principals"`
	Mappings   map[string]string    `json:"role_mappings"`
	OPA        OPAConnection        `json:"opa"`
	Channel    string               `json:"channel"`
	AgentID    domain.ID            `json:"agent_id"`
	AgentName  string               `json:"agent_name"`
	Manifest   domain.AgentManifest `json:"manifest"`
}
type BootstrapResult struct {
	TenantID      domain.ID `json:"tenant_id"`
	AgentID       domain.ID `json:"agent_id"`
	VersionDigest string    `json:"version_digest"`
	PolicyDigest  string    `json:"policy_digest"`
}
type BootstrapRepository interface {
	PolicyAdministrationStore
	AgentRegistry
	AgentVersionRegistry
	AgentApprovalRegistry
}
type BootstrapStore interface {
	Provision(context.Context, BootstrapSpec, PolicyArtifact, string, time.Time, func(BootstrapRepository) error) (BootstrapResult, error)
}
