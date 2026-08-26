package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type ResourceExtensionEvidence struct {
	AuditID, OutboxID, EventID, RequestID domain.ID
	ReasonCodes                           []string
}

type ResourceExtensionRepository interface {
	RunQueryRepository
	ExtendResources(context.Context, domain.ResourceExtension, ResourceExtensionEvidence) (domain.ResourceExtensionResult, error)
}
