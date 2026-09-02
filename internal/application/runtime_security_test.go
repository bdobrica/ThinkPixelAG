package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type runtimeSecuritySourceStub struct {
	states []ports.RuntimeSecurityState
	err    error
}

func (s *runtimeSecuritySourceStub) LoadRuntimeSecurityState(context.Context, time.Time) ([]ports.RuntimeSecurityState, error) {
	return s.states, s.err
}

func TestRuntimeSecurityReadinessRequiresPolicyAndFreshRevocations(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	clock := &freshnessTestClock{wall: now}
	policies, _ := policy.NewFreshness(time.Minute, clock.Now)
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	source := &runtimeSecuritySourceStub{}
	readiness, err := NewRuntimeSecurityReadiness(source, policies, tracker, clock, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = readiness.Ready(context.Background()); err == nil {
		t.Fatal("unreconciled security state reported ready")
	}
	if err = readiness.Refresh(context.Background()); err == nil {
		t.Fatal("empty active policy set was accepted")
	}
	tenant, _ := domain.ParseID("019feba6-b9bb-7fff-bfff-ffffffffffff")
	source.states = []ports.RuntimeSecurityState{{
		Policy:   ports.RuntimeActivePolicy{TenantID: tenant.String(), Channel: "stable", BundleID: "019feba6-b9bb-7fff-bfff-fffffffffffe", Digest: "sha256:policy", Version: 1, ActivatedAt: now},
		Sequence: 7, Epochs: domain.EpochVector{Security: 3, TenantPolicy: 2, TenantRevocation: 1},
	}}
	if err = readiness.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = readiness.Ready(context.Background()); err != nil {
		t.Fatalf("fresh security state was not ready: %v", err)
	}
	clock.advance(31*time.Second, 31*time.Second)
	if err = readiness.Ready(context.Background()); err == nil {
		t.Fatal("stale revocation state reported ready")
	}
}

func TestRuntimeSecurityReadinessFailsClosedOnRefreshFailure(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	clock := &freshnessTestClock{wall: now}
	policies, _ := policy.NewFreshness(time.Minute, clock.Now)
	tracker, _ := NewRevocationFreshnessTrackerWithElapsed(clock, clock.monotonic)
	tenant := "019feba6-b9bb-7fff-bfff-ffffffffffff"
	source := &runtimeSecuritySourceStub{states: []ports.RuntimeSecurityState{{Policy: ports.RuntimeActivePolicy{TenantID: tenant, Channel: "stable", BundleID: tenant, Digest: "sha256:policy", Version: 1, ActivatedAt: now}}}}
	readiness, _ := NewRuntimeSecurityReadiness(source, policies, tracker, clock, 30*time.Second)
	if err := readiness.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	source.err = errors.New("database unavailable")
	if err := readiness.Refresh(context.Background()); err == nil {
		t.Fatal("refresh failure was hidden")
	}
	if err := readiness.Ready(context.Background()); err == nil {
		t.Fatal("last successful state remained ready after refresh failure")
	}
}
