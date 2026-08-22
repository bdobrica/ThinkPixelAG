package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type AgentApprovalRegistry interface {
	RecordAgentVersionDecision(context.Context, domain.AgentVersionApproval, string, domain.ID, domain.ID, *domain.ID) (domain.AgentVersionApproval, error)
	AgentVersionEligibility(context.Context, domain.ID, string) (domain.AgentVersionState, error)
}
