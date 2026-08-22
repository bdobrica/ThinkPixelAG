package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type AgentVersionRegistry interface {
	RegisterAgentVersion(context.Context, domain.AgentVersion, []domain.AgentCapability) error
	DescribeAgentVersion(context.Context, domain.ID, string) (domain.AgentVersion, []domain.AgentCapability, error)
}
