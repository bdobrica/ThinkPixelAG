package ports

import (
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type RegistryRepository interface {
	AgentRegistry
	AgentVersionRegistry
	AgentApprovalRegistry
}
type RegistryAdministrationStore interface {
	RegistryTransaction(context.Context, AdministrationOperation, func(RegistryRepository) (any, error)) (json.RawMessage, error)
}
type RunListRepository interface {
	ListRunIDs(context.Context, domain.ID, int) ([]domain.ID, error)
}
