package ports

import (
	"context"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type AgentListQuery struct {
	After domain.ID
	Limit int
}

type PrincipalEligibility struct {
	Exists   bool
	Disabled bool
}

type AgentRegistry interface {
	PrincipalEligibility(context.Context, domain.ID) (PrincipalEligibility, error)
	CreateAgent(context.Context, domain.Agent) error
	UpdateAgent(context.Context, domain.Agent, time.Time) error
	DescribeAgent(context.Context, domain.ID) (domain.Agent, error)
	ListAgents(context.Context, AgentListQuery) ([]domain.Agent, error)
}
