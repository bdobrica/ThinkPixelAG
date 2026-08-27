package application

import (
	"errors"
	"sync"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

var (
	ErrInvalidFreshnessObservation = errors.New("invalid revocation freshness observation")
	ErrFreshnessRegression         = errors.New("revocation freshness must not regress")
	ErrMonotonicClockRegression    = errors.New("revocation freshness monotonic clock regressed")
)

// RevocationFreshness is an immutable view of the revocation authority state
// applied by this process for one tenant. Age is derived only from the
// process-local monotonic clock; the UTC timestamps are diagnostic metadata.
type RevocationFreshness struct {
	TenantID              domain.ID
	LastAppliedSequence   int64
	LastAppliedEpochs     domain.EpochVector
	LastStreamReceipt     time.Time
	LastReconciliation    time.Time
	Age                   time.Duration
	HasAuthoritativeState bool
	MonotonicClockHealthy bool
}

type revocationFreshnessState struct {
	lastAppliedSequence int64
	lastAppliedEpochs   domain.EpochVector
	lastStreamReceipt   time.Time
	lastReconciliation  time.Time
	observedElapsed     time.Duration
}

// RevocationFreshnessTracker stores only instance-local state. elapsed must be
// monotonic for the lifetime of the process (for example time.Since(start)).
// Keeping it separate from the wall clock prevents NTP or administrator clock
// changes from extending an authorization freshness window.
type RevocationFreshnessTracker struct {
	mu           sync.RWMutex
	clock        domain.Clock
	elapsed      func() time.Duration
	states       map[domain.ID]revocationFreshnessState
	invalidators []func(string)
}

// AddCacheInvalidator registers a synchronous notification invoked after a
// revocation observation is accepted. Notifications run outside the tracker
// lock and should only update disposable cache state.
func (t *RevocationFreshnessTracker) AddCacheInvalidator(invalidate func(string)) {
	if t == nil || invalidate == nil {
		return
	}
	t.mu.Lock()
	t.invalidators = append(t.invalidators, invalidate)
	t.mu.Unlock()
}

func NewRevocationFreshnessTracker(clock domain.Clock) (*RevocationFreshnessTracker, error) {
	started := time.Now()
	return NewRevocationFreshnessTrackerWithElapsed(clock, func() time.Duration { return time.Since(started) })
}

func NewRevocationFreshnessTrackerWithElapsed(clock domain.Clock, elapsed func() time.Duration) (*RevocationFreshnessTracker, error) {
	if clock == nil || elapsed == nil {
		return nil, errors.New("revocation freshness requires wall and monotonic clocks")
	}
	return &RevocationFreshnessTracker{clock: clock, elapsed: elapsed, states: make(map[domain.ID]revocationFreshnessState)}, nil
}

func (t *RevocationFreshnessTracker) RecordStreamReceipt(tenant domain.ID, sequence int64, epochs domain.EpochVector) error {
	return t.record(tenant, sequence, epochs, true)
}

func (t *RevocationFreshnessTracker) RecordReconciliation(tenant domain.ID, sequence int64, epochs domain.EpochVector) error {
	return t.record(tenant, sequence, epochs, false)
}

func (t *RevocationFreshnessTracker) record(tenant domain.ID, sequence int64, epochs domain.EpochVector, stream bool) error {
	if tenant.IsZero() || sequence < 0 || !validEpochVector(epochs) {
		return ErrInvalidFreshnessObservation
	}
	wall, err := domain.RequireUTC(t.clock.Now())
	if err != nil || wall.IsZero() {
		return ErrInvalidFreshnessObservation
	}
	elapsed := t.elapsed()
	if elapsed < 0 {
		return ErrMonotonicClockRegression
	}

	t.mu.Lock()
	previous, exists := t.states[tenant]
	if exists {
		if elapsed < previous.observedElapsed {
			t.mu.Unlock()
			return ErrMonotonicClockRegression
		}
		if sequence < previous.lastAppliedSequence || epochs.Security < previous.lastAppliedEpochs.Security {
			t.mu.Unlock()
			return ErrFreshnessRegression
		}
		if !stream && epochRegresses(epochs, previous.lastAppliedEpochs) {
			t.mu.Unlock()
			return ErrFreshnessRegression
		}
		if sequence == previous.lastAppliedSequence && epochAdvances(epochs, previous.lastAppliedEpochs) {
			t.mu.Unlock()
			return ErrFreshnessRegression
		}
	}
	previous.lastAppliedSequence = sequence
	previous.lastAppliedEpochs = mergeEpochs(previous.lastAppliedEpochs, epochs)
	previous.observedElapsed = elapsed
	if stream {
		previous.lastStreamReceipt = wall
	} else {
		previous.lastReconciliation = wall
	}
	t.states[tenant] = previous
	invalidators := append([]func(string){}, t.invalidators...)
	t.mu.Unlock()
	for _, invalidate := range invalidators {
		invalidate(tenant.String())
	}
	return nil
}

func (t *RevocationFreshnessTracker) Snapshot(tenant domain.ID) RevocationFreshness {
	out := RevocationFreshness{TenantID: tenant, MonotonicClockHealthy: true}
	if t == nil || tenant.IsZero() {
		out.MonotonicClockHealthy = false
		return out
	}
	t.mu.RLock()
	state, exists := t.states[tenant]
	t.mu.RUnlock()
	if !exists {
		return out
	}
	now := t.elapsed()
	out.LastAppliedSequence = state.lastAppliedSequence
	out.LastAppliedEpochs = state.lastAppliedEpochs
	out.LastStreamReceipt = state.lastStreamReceipt
	out.LastReconciliation = state.lastReconciliation
	out.HasAuthoritativeState = true
	if now < state.observedElapsed {
		out.MonotonicClockHealthy = false
		out.HasAuthoritativeState = false
		return out
	}
	out.Age = now - state.observedElapsed
	return out
}

func validEpochVector(v domain.EpochVector) bool {
	return v.Security >= 0 && v.TenantPolicy >= 0 && v.TenantRevocation >= 0 && v.AgentRevocation >= 0
}

func epochRegresses(next, previous domain.EpochVector) bool {
	return next.Security < previous.Security || next.TenantPolicy < previous.TenantPolicy || next.TenantRevocation < previous.TenantRevocation || next.AgentRevocation < previous.AgentRevocation
}

func epochAdvances(next, previous domain.EpochVector) bool {
	return next.Security > previous.Security || next.TenantPolicy > previous.TenantPolicy || next.TenantRevocation > previous.TenantRevocation || next.AgentRevocation > previous.AgentRevocation
}

func mergeEpochs(current, observed domain.EpochVector) domain.EpochVector {
	if observed.Security > current.Security {
		current.Security = observed.Security
	}
	if observed.TenantPolicy > current.TenantPolicy {
		current.TenantPolicy = observed.TenantPolicy
	}
	if observed.TenantRevocation > current.TenantRevocation {
		current.TenantRevocation = observed.TenantRevocation
	}
	if observed.AgentRevocation > current.AgentRevocation {
		current.AgentRevocation = observed.AgentRevocation
	}
	return current
}
