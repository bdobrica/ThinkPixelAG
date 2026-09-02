package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type operationalMetricsSourceStub struct {
	snapshot ports.OperationalMetricsSnapshot
	err      error
}

func (s *operationalMetricsSourceStub) LoadOperationalMetrics(context.Context, time.Time) (ports.OperationalMetricsSnapshot, error) {
	return s.snapshot, s.err
}

type operationalMetricsSinkStub struct {
	pending int
	oldest  time.Duration
	lag     time.Duration
	calls   int
}

func (s *operationalMetricsSinkStub) SetOutbox(pending int, oldest time.Duration) {
	s.pending, s.oldest = pending, oldest
	s.calls++
}
func (s *operationalMetricsSinkStub) SetSettlementLag(lag time.Duration) { s.lag = lag }

func TestOperationalMetricsPublisherRefreshesBoundedBacklogSignals(t *testing.T) {
	source := &operationalMetricsSourceStub{snapshot: ports.OperationalMetricsSnapshot{OutboxPending: 7, OutboxOldestAge: 42 * time.Second, OldestSettlementLag: 9 * time.Second}}
	sink := &operationalMetricsSinkStub{}
	publisher, err := NewOperationalMetricsPublisher(source, sink, fixedClock{now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if err = publisher.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sink.pending != 7 || sink.oldest != 42*time.Second || sink.lag != 9*time.Second || sink.calls != 1 {
		t.Fatalf("published metrics = %+v", sink)
	}
	source.err = errors.New("database unavailable")
	if err = publisher.Refresh(context.Background()); err == nil || sink.calls != 1 {
		t.Fatalf("failed refresh changed the last sample: err=%v sink=%+v", err, sink)
	}
}

func TestOperationalMetricsPublisherRejectsMissingDependencies(t *testing.T) {
	if _, err := NewOperationalMetricsPublisher(nil, &operationalMetricsSinkStub{}, fixedClock{now: time.Now().UTC()}); err == nil {
		t.Fatal("missing source was accepted")
	}
}
