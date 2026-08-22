package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

// AgentDiscoveryRepository is the tenant-bound read model used by public
// discovery. Version candidates carry the approval evidence needed to avoid
// publishing agents that cannot be invoked.
type AgentDiscoveryRepository interface {
	DescribeAgent(context.Context, domain.ID) (domain.Agent, error)
	ListAgents(context.Context, AgentListQuery) ([]domain.Agent, error)
	ListAgentVersionCandidates(context.Context, domain.ID, string) ([]domain.AgentVersionCandidate, error)
}
