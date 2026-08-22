package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type RunEventWindow struct {
	Events                 []domain.RunEvent
	OldestRetained, Latest int64
}

type RunEventRepository interface {
	ListRunEvents(context.Context, domain.ID, int64, int) (RunEventWindow, error)
}
