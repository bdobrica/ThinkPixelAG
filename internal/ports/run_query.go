package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type RunAccessRecord struct {
	Run            domain.Run
	AgentRiskClass domain.AgentRiskClass
	AgentOwnerID   domain.ID
}

type RunQueryRepository interface {
	GetRun(context.Context, domain.ID) (RunAccessRecord, error)
}
