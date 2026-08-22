package ports

import (
	"context"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type RunWorkerRepository interface {
	ClaimRun(context.Context, domain.ID, domain.ID, domain.ID, time.Time, time.Time) (domain.RunLease, error)
	HeartbeatRun(context.Context, domain.RunLease, time.Time, time.Time) (domain.RunLease, error)
	MutateWorkerRun(context.Context, domain.RunLease, domain.WorkerRunOperation, domain.ID, time.Time) (domain.Run, error)
}
