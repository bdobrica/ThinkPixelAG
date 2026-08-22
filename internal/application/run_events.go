package application

import (
	"context"
	"errors"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type RunEventStream struct {
	access *RunQuery
	events ports.RunEventRepository
	retain int64
}

func NewRunEventStream(access *RunQuery, events ports.RunEventRepository, retainedEvents int64) (*RunEventStream, error) {
	if access == nil || events == nil || retainedEvents < 1 {
		return nil, errors.New("run event stream requires access, repository, and positive retention")
	}
	return &RunEventStream{access: access, events: events, retain: retainedEvents}, nil
}

func (s *RunEventStream) Authorize(ctx context.Context, command GetRun) error {
	_, err := s.access.Get(ctx, command)
	return err
}

func (s *RunEventStream) Events(ctx context.Context, runID domain.ID, after int64, limit int) ([]domain.RunEvent, error) {
	if runID.IsZero() || after < 0 || limit < 1 || limit > 256 {
		return nil, domain.NewError(domain.CodeInvalidArgument, "run event request is invalid")
	}
	window, err := s.events.ListRunEvents(ctx, runID, after, limit)
	if err != nil {
		return nil, runNotFound(err)
	}
	retentionFloor := window.Latest - s.retain
	if retentionFloor < 0 {
		retentionFloor = 0
	}
	if physicalFloor := window.OldestRetained - 1; physicalFloor > retentionFloor {
		retentionFloor = physicalFloor
	}
	// Sequence zero is the well-known beginning used only when no cursor was
	// supplied. It starts at the oldest retained event. Every issued cursor is
	// positive and must remain within the window.
	if after == 0 && retentionFloor > 0 {
		window, err = s.events.ListRunEvents(ctx, runID, retentionFloor, limit)
		if err != nil {
			return nil, runNotFound(err)
		}
	} else if after < retentionFloor {
		return nil, domain.ErrRunEventCursorGone
	}
	previous := after
	if previous < retentionFloor {
		previous = retentionFloor
	}
	for _, event := range window.Events {
		if event.Validate() != nil || event.RunID != runID || event.Sequence <= previous {
			return nil, domain.NewError(domain.CodeInternal, "run event repository returned invalid ordering")
		}
		previous = event.Sequence
	}
	return window.Events, nil
}
