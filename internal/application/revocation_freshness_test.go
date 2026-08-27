package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

func TestRevocationFreshnessMetricsAndReadinessTrackAgeLagAndGap(t *testing.T) {
	tenant := applicationID(t)
	clock := &freshnessTestClock{wall: time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)}
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	readiness, err := NewRevocationReadiness(tracker, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tracker.TrackTenant(tenant)
	if err = readiness.Ready(context.Background()); err == nil {
		t.Fatal("unknown post-restart state was ready")
	}
	if err = tracker.RecordReconciliation(tenant, 10, domain.EpochVector{Security: 3}); err != nil {
		t.Fatal(err)
	}
	if err = readiness.Ready(context.Background()); err != nil {
		t.Fatalf("fresh state not ready: %v", err)
	}
	if err = tracker.RecordAuthoritativeHead(tenant, 12); err != nil {
		t.Fatal(err)
	}
	if status := tracker.Metrics(30 * time.Second); status.MaximumLag != 2 || status.Healthy {
		t.Fatalf("lag status = %+v", status)
	}
	if err = tracker.RecordGap(tenant, 13); err != nil {
		t.Fatal(err)
	}
	status := tracker.Metrics(30 * time.Second)
	if status.CurrentGaps != 1 || status.GapEvents != 1 || status.MaximumLag != 3 {
		t.Fatalf("gap status = %+v", status)
	}
	if err = tracker.RecordReconciliation(tenant, 13, domain.EpochVector{Security: 4}); err != nil {
		t.Fatal(err)
	}
	clock.advance(31*time.Second, 31*time.Second)
	status = tracker.Metrics(30 * time.Second)
	if status.CurrentGaps != 0 || status.MaximumLag != 0 || status.MaximumAge != 31*time.Second || status.Healthy {
		t.Fatalf("stale recovery status = %+v", status)
	}
}

func TestRevocationFreshnessRejectsRegressingAuthoritativeHead(t *testing.T) {
	tracker, _ := NewRevocationFreshnessTracker(fixedClock{now: time.Now().UTC()})
	tenant := applicationID(t)
	if err := tracker.RecordAuthoritativeHead(tenant, 9); err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordAuthoritativeHead(tenant, 8); !errors.Is(err, ErrFreshnessRegression) {
		t.Fatalf("regressing head error = %v", err)
	}
}

type freshnessTestClock struct {
	mu      sync.Mutex
	wall    time.Time
	elapsed time.Duration
}

func (c *freshnessTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wall
}
func (c *freshnessTestClock) monotonic() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.elapsed
}
func (c *freshnessTestClock) advance(wall, elapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wall = c.wall.Add(wall)
	c.elapsed += elapsed
}

func TestRevocationFreshnessTracksStreamAndReconciliation(t *testing.T) {
	tenant := applicationID(t)
	clock := &freshnessTestClock{wall: time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)}
	tracker, err := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	if err != nil {
		t.Fatal(err)
	}
	if state := tracker.Snapshot(tenant); state.HasAuthoritativeState || !state.MonotonicClockHealthy {
		t.Fatalf("unexpected initial state: %+v", state)
	}

	streamEpochs := domain.EpochVector{Security: 4, TenantPolicy: 2, TenantRevocation: 3}
	if err = tracker.RecordStreamReceipt(tenant, 9, streamEpochs); err != nil {
		t.Fatal(err)
	}
	clock.advance(20*time.Second, 20*time.Second)
	stream := tracker.Snapshot(tenant)
	if !stream.HasAuthoritativeState || stream.Age != 20*time.Second || stream.LastStreamReceipt.IsZero() || !stream.LastReconciliation.IsZero() || stream.LastAppliedSequence != 9 || stream.LastAppliedEpochs != streamEpochs {
		t.Fatalf("unexpected stream state: %+v", stream)
	}

	reconciledEpochs := domain.EpochVector{Security: 5, TenantPolicy: 2, TenantRevocation: 4}
	if err = tracker.RecordReconciliation(tenant, 12, reconciledEpochs); err != nil {
		t.Fatal(err)
	}
	clock.advance(7*time.Second, 7*time.Second)
	reconciled := tracker.Snapshot(tenant)
	if reconciled.Age != 7*time.Second || reconciled.LastReconciliation.IsZero() || reconciled.LastStreamReceipt != stream.LastStreamReceipt || reconciled.LastAppliedSequence != 12 || reconciled.LastAppliedEpochs != reconciledEpochs {
		t.Fatalf("unexpected reconciled state: %+v", reconciled)
	}
}

func TestRevocationFreshnessAgeIgnoresWallClockAdjustment(t *testing.T) {
	tenant := applicationID(t)
	clock := &freshnessTestClock{wall: time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)}
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	if err := tracker.RecordStreamReceipt(tenant, 1, domain.EpochVector{Security: 1}); err != nil {
		t.Fatal(err)
	}
	clock.advance(-6*time.Hour, 45*time.Second)
	state := tracker.Snapshot(tenant)
	if state.Age != 45*time.Second || !state.HasAuthoritativeState {
		t.Fatalf("wall-clock rollback changed freshness: %+v", state)
	}

	clock.advance(0, -time.Minute)
	state = tracker.Snapshot(tenant)
	if state.HasAuthoritativeState || state.MonotonicClockHealthy {
		t.Fatalf("monotonic regression did not fail closed: %+v", state)
	}
}

func TestRevocationFreshnessRejectsSequenceEpochAndClockRegression(t *testing.T) {
	tenant := applicationID(t)
	clock := &freshnessTestClock{wall: time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC), elapsed: time.Minute}
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	epochs := domain.EpochVector{Security: 3, TenantPolicy: 2, TenantRevocation: 2}
	if err := tracker.RecordReconciliation(tenant, 8, epochs); err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordStreamReceipt(tenant, 7, epochs); !errors.Is(err, ErrFreshnessRegression) {
		t.Fatalf("sequence regression error = %v", err)
	}
	if err := tracker.RecordStreamReceipt(tenant, 8, domain.EpochVector{Security: 4, TenantPolicy: 2, TenantRevocation: 2}); !errors.Is(err, ErrFreshnessRegression) {
		t.Fatalf("same-sequence conflict error = %v", err)
	}
	if err := tracker.RecordStreamReceipt(tenant, 9, domain.EpochVector{Security: 2}); !errors.Is(err, ErrFreshnessRegression) {
		t.Fatalf("epoch regression error = %v", err)
	}
	clock.advance(0, -time.Second)
	if err := tracker.RecordStreamReceipt(tenant, 8, epochs); !errors.Is(err, ErrMonotonicClockRegression) {
		t.Fatalf("clock regression error = %v", err)
	}
}

func TestRevocationFreshnessMergesSparseStreamEpochs(t *testing.T) {
	tenant := applicationID(t)
	clock := &freshnessTestClock{wall: time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)}
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	if err := tracker.RecordStreamReceipt(tenant, 3, domain.EpochVector{Security: 3, TenantPolicy: 2, TenantRevocation: 2, AgentRevocation: 4}); err != nil {
		t.Fatal(err)
	}
	// Log entries carry zero for epoch dimensions not affected by that change.
	if err := tracker.RecordStreamReceipt(tenant, 4, domain.EpochVector{Security: 4}); err != nil {
		t.Fatal(err)
	}
	want := domain.EpochVector{Security: 4, TenantPolicy: 2, TenantRevocation: 2, AgentRevocation: 4}
	if got := tracker.Snapshot(tenant).LastAppliedEpochs; got != want {
		t.Fatalf("sparse stream epochs replaced applied state: got %+v want %+v", got, want)
	}
}

func TestRevocationFreshnessIsTenantScopedAndConcurrent(t *testing.T) {
	clock := &freshnessTestClock{wall: time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)}
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	tenantA, tenantB := applicationID(t), applicationID(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := tracker.RecordStreamReceipt(tenantA, 1, domain.EpochVector{Security: 1}); err != nil {
				t.Errorf("record tenant A: %v", err)
			}
		}()
	}
	if err := tracker.RecordReconciliation(tenantB, 2, domain.EpochVector{Security: 2}); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if tracker.Snapshot(tenantA).LastAppliedSequence != 1 || tracker.Snapshot(tenantB).LastAppliedSequence != 2 {
		t.Fatal("tenant freshness state was not isolated")
	}
}

func TestRevocationFreshnessInvalidatesCacheAfterAcceptedObservation(t *testing.T) {
	clock := &freshnessTestClock{wall: time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)}
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	tenant := applicationID(t)
	var invalidated []string
	tracker.AddCacheInvalidator(func(got string) { invalidated = append(invalidated, got) })
	if err := tracker.RecordStreamReceipt(tenant, 1, domain.EpochVector{Security: 1, TenantRevocation: 1}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.RecordStreamReceipt(tenant, 0, domain.EpochVector{}); err == nil {
		t.Fatal("accepted regressing observation")
	}
	if len(invalidated) != 1 || invalidated[0] != tenant.String() {
		t.Fatalf("cache invalidations=%v", invalidated)
	}
}
