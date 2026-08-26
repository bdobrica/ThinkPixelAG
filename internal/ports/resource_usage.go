package ports

import (
	"context"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type TrustedUsageRepository interface {
	RunQueryRepository
	RecordTrustedUsage(context.Context, domain.TrustedUsage, domain.ThroughputHint) (domain.UsageReceipt, error)
}

type ThroughputAccelerator interface {
	Blocked(context.Context, string) (bool, error)
	MarkBlocked(context.Context, string, time.Time) error
}
