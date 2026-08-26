package ports

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type TrustedUsageRepository interface {
	RunQueryRepository
	RecordTrustedUsage(context.Context, domain.TrustedUsage) (domain.UsageReceipt, error)
}
