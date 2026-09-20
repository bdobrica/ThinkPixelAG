package ports

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"time"
)

// PolicyArtifact contains protected source; only explicit administration source
// endpoints expose it. Evidence and idempotency responses contain summaries.
type PolicyArtifact struct {
	ID              domain.ID `json:"-"`
	Digest          string    `json:"digest"`
	ContractVersion string    `json:"contract_version"`
	Revision        uint64    `json:"artifact_revision"`
	Source          []byte    `json:"-"`
	Signature       Signature `json:"-"`
	Channel         string    `json:"-"`
	State           string    `json:"state"`
	CreatedAt       time.Time `json:"created_at"`
}
type PolicyActivation struct {
	ID          domain.ID `json:"id"`
	Digest      string    `json:"policy_digest"`
	Channel     string    `json:"channel"`
	Version     int64     `json:"policy_epoch"`
	ActivatedAt time.Time `json:"activated_at"`
}
type AdministrationOperation struct {
	TenantID, ActorID, RequestID, DecisionID                  domain.ID
	Action, Resource, Channel, Key, RequestHash, PolicyDigest string
	PolicyVersion                                             int64
	At                                                        time.Time
}
type PolicyAdministrationStore interface {
	PolicyArtifact(context.Context, string, string) (PolicyArtifact, error)
	CurrentPolicy(context.Context, string) (PolicyArtifact, PolicyActivation, error)
	StorePolicy(context.Context, AdministrationOperation, PolicyArtifact) (PolicyArtifact, error)
	ActivatePolicy(context.Context, AdministrationOperation, string, string) (PolicyActivation, error)
}

// PolicyModuleRuntime loads immutable compiled modules, without selecting them
// as authority. Selection remains the active PostgreSQL record.
type PolicyModuleRuntime interface {
	Validate(context.Context, []byte) error
	Ensure(context.Context, string, []byte) error
}
