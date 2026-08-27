package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

var (
	ErrInvalidFreshnessObservation = errors.New("invalid revocation freshness observation")
	ErrFreshnessRegression         = errors.New("revocation freshness must not regress")
	ErrFreshnessGap                = errors.New("revocation freshness sequence gap")
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
	lastAppliedSequence   int64
	lastAppliedEpochs     domain.EpochVector
	lastStreamReceipt     time.Time
	lastReconciliation    time.Time
	observedElapsed       time.Duration
	authoritativeSequence int64
	gap                   bool
	hasObservation        bool
}

// RevocationFreshnessMetrics is an aggregate, bounded-cardinality operational
// view. Tenant identifiers are deliberately excluded from metric dimensions.
type RevocationFreshnessMetrics struct {
	MaximumAge     time.Duration
	MaximumLag     int64
	CurrentGaps    int
	GapEvents      uint64
	TrackedTenants int
	Healthy        bool
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
	gapEvents    uint64
}

// TrackTenant makes a tenant part of the instance readiness contract. A newly
// tracked tenant starts unknown and therefore fails closed until authority is
// observed by stream or reconciliation.
func (t *RevocationFreshnessTracker) TrackTenant(tenant domain.ID) {
	if t == nil || tenant.IsZero() {
		return
	}
	t.mu.Lock()
	if _, ok := t.states[tenant]; !ok {
		t.states[tenant] = revocationFreshnessState{}
	}
	t.mu.Unlock()
}

// RecordAuthoritativeHead records a known authority high-water mark. A head
// beyond the applied sequence exposes lag without treating it as a gap.
func (t *RevocationFreshnessTracker) RecordAuthoritativeHead(tenant domain.ID, sequence int64) error {
	if t == nil || tenant.IsZero() || sequence < 0 {
		return ErrInvalidFreshnessObservation
	}
	t.mu.Lock()
	state := t.states[tenant]
	if sequence < state.authoritativeSequence || (state.lastAppliedSequence > 0 && sequence < state.lastAppliedSequence) {
		t.mu.Unlock()
		return ErrFreshnessRegression
	}
	state.authoritativeSequence = sequence
	t.states[tenant] = state
	t.mu.Unlock()
	return nil
}

// RecordGap marks a tenant unsafe until a successful reconciliation. Repeated
// signals keep the gauge set and count each detected incident for alerting.
func (t *RevocationFreshnessTracker) RecordGap(tenant domain.ID, authoritativeSequence int64) error {
	if t == nil || tenant.IsZero() || authoritativeSequence < 0 {
		return ErrInvalidFreshnessObservation
	}
	t.mu.Lock()
	state := t.states[tenant]
	if authoritativeSequence < state.authoritativeSequence || (state.lastAppliedSequence > 0 && authoritativeSequence < state.lastAppliedSequence) {
		t.mu.Unlock()
		return ErrFreshnessRegression
	}
	state.authoritativeSequence = authoritativeSequence
	state.gap = true
	t.states[tenant] = state
	t.gapEvents++
	t.mu.Unlock()
	return nil
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
		if stream && previous.hasObservation && sequence == previous.lastAppliedSequence {
			// Delivery is at least once. An exact duplicate is already applied,
			// but must not extend the freshness window during a partition.
			t.mu.Unlock()
			return nil
		}
		if stream && previous.hasObservation && sequence > previous.lastAppliedSequence+1 {
			// Do not apply across a missing sequence. Preserve the last fully
			// applied state and require an authoritative reconciliation.
			if sequence > previous.authoritativeSequence {
				previous.authoritativeSequence = sequence
			}
			previous.gap = true
			t.states[tenant] = previous
			t.gapEvents++
			t.mu.Unlock()
			return ErrFreshnessGap
		}
	}
	previous.lastAppliedSequence = sequence
	previous.hasObservation = true
	if sequence > previous.authoritativeSequence {
		previous.authoritativeSequence = sequence
	}
	previous.lastAppliedEpochs = mergeEpochs(previous.lastAppliedEpochs, epochs)
	previous.observedElapsed = elapsed
	if stream {
		previous.lastStreamReceipt = wall
	} else {
		previous.lastReconciliation = wall
		previous.gap = false
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
	if !exists || !state.hasObservation {
		return out
	}
	now := t.elapsed()
	out.LastAppliedSequence = state.lastAppliedSequence
	out.LastAppliedEpochs = state.lastAppliedEpochs
	out.LastStreamReceipt = state.lastStreamReceipt
	out.LastReconciliation = state.lastReconciliation
	out.HasAuthoritativeState = true
	if state.gap || state.authoritativeSequence > state.lastAppliedSequence {
		out.HasAuthoritativeState = false
		return out
	}
	if now < state.observedElapsed {
		out.MonotonicClockHealthy = false
		out.HasAuthoritativeState = false
		return out
	}
	out.Age = now - state.observedElapsed
	return out
}

// Metrics returns a scrape-time aggregate so age continues advancing during a
// partition even when no observation method is called.
func (t *RevocationFreshnessTracker) Metrics(maxAge time.Duration) RevocationFreshnessMetrics {
	out := RevocationFreshnessMetrics{Healthy: true}
	if t == nil || maxAge <= 0 {
		out.Healthy = false
		return out
	}
	t.mu.RLock()
	states := make([]revocationFreshnessState, 0, len(t.states))
	for _, state := range t.states {
		states = append(states, state)
	}
	out.GapEvents = t.gapEvents
	t.mu.RUnlock()
	out.TrackedTenants = len(states)
	if len(states) == 0 {
		out.Healthy = false
	}
	now := t.elapsed()
	for _, state := range states {
		age := time.Duration(0)
		clockHealthy := now >= state.observedElapsed
		if clockHealthy {
			age = now - state.observedElapsed
		}
		if age > out.MaximumAge {
			out.MaximumAge = age
		}
		lag := state.authoritativeSequence - state.lastAppliedSequence
		if lag > out.MaximumLag {
			out.MaximumLag = lag
		}
		if state.gap {
			out.CurrentGaps++
		}
		if !state.hasObservation || !clockHealthy || age > maxAge || lag > 0 || state.gap {
			out.Healthy = false
		}
	}
	return out
}

// RevocationReadiness gates governed traffic at the minimum freshness class
// configured for this process. It intentionally has no liveness behavior.
type RevocationReadiness struct {
	tracker *RevocationFreshnessTracker
	maxAge  time.Duration
}

func NewRevocationReadiness(tracker *RevocationFreshnessTracker, maxAge time.Duration) (*RevocationReadiness, error) {
	if tracker == nil || maxAge <= 0 {
		return nil, errors.New("revocation readiness requires tracker and maximum age")
	}
	return &RevocationReadiness{tracker: tracker, maxAge: maxAge}, nil
}

func (r *RevocationReadiness) Ready(context.Context) error {
	status := r.tracker.Metrics(r.maxAge)
	if !status.Healthy {
		return fmt.Errorf("revocation security state is not fresh: age=%s lag=%d gaps=%d tenants=%d", status.MaximumAge, status.MaximumLag, status.CurrentGaps, status.TrackedTenants)
	}
	return nil
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
