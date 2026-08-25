package ports

import (
	"context"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

// ResourceReservationRepository atomically delegates a consumable vector to
// a child envelope. Authorization and child-admission composition live above
// this persistence boundary.
type ResourceReservationRepository interface {
	ReserveChildResources(context.Context, domain.ResourceReservation) (domain.ResourceReservation, error)
}
