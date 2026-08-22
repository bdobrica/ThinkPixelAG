package application

import (
	"context"
	"errors"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type runEventRepositoryStub struct {
	window ports.RunEventWindow
	err    error
	after  int64
	limit  int
}

func (s *runEventRepositoryStub) ListRunEvents(_ context.Context, _ domain.ID, after int64, limit int) (ports.RunEventWindow, error) {
	s.after, s.limit = after, limit
	return s.window, s.err
}

func TestRunEventStreamOrdersPageAndEnforcesRetention(t *testing.T) {
	runID := applicationID(t)
	now := testTime()
	repo := &runEventRepositoryStub{window: ports.RunEventWindow{Latest: 12, Events: []domain.RunEvent{{ID: applicationID(t), RunID: runID, Sequence: 11, Type: "run.started", Data: map[string]any{}, OccurredAt: now}, {ID: applicationID(t), RunID: runID, Sequence: 12, Type: "run.completed", Data: map[string]any{}, OccurredAt: now}}}}
	service := &RunEventStream{events: repo, retain: 10}
	events, err := service.Events(context.Background(), runID, 10, 20)
	if err != nil || len(events) != 2 || repo.after != 10 || repo.limit != 20 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	_, err = service.Events(context.Background(), runID, 1, 20)
	if !errors.Is(err, domain.ErrRunEventCursorGone) {
		t.Fatalf("retention err=%v", err)
	}
}
