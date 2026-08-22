package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type VersionResolutionRepository interface {
	ListAgentVersionCandidates(context.Context, domain.ID, string) ([]domain.AgentVersionCandidate, error)
	PersistRunVersionResolution(context.Context, domain.RunVersionResolution) error
	DescribeRunVersionResolution(context.Context, domain.ID) (domain.RunVersionResolution, error)
}
