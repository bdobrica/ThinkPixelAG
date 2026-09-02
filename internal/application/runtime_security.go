package application

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

// RuntimeSecurityReadiness periodically reconciles process-local policy and
// revocation state. It is a readiness dependency only; transient failures do
// not affect liveness or force a restart loop.
type RuntimeSecurityReadiness struct {
	source      ports.RuntimeSecurityStateSource
	policies    *policy.Freshness
	revocations *RevocationFreshnessTracker
	clock       domain.Clock
	maxAge      time.Duration

	mu      sync.RWMutex
	active  []policy.ActiveBundle
	lastErr error
}

func NewRuntimeSecurityReadiness(source ports.RuntimeSecurityStateSource, policies *policy.Freshness, revocations *RevocationFreshnessTracker, clock domain.Clock, maxAge time.Duration) (*RuntimeSecurityReadiness, error) {
	if source == nil || policies == nil || revocations == nil || clock == nil || maxAge <= 0 {
		return nil, errors.New("runtime security readiness dependencies are required")
	}
	return &RuntimeSecurityReadiness{source: source, policies: policies, revocations: revocations, clock: clock, maxAge: maxAge, lastErr: errors.New("security state has not been reconciled")}, nil
}

func (r *RuntimeSecurityReadiness) Refresh(ctx context.Context) error {
	now := r.clock.Now()
	states, err := r.source.LoadRuntimeSecurityState(ctx, now)
	if err == nil && len(states) == 0 {
		err = errors.New("no active policy is loaded")
	}
	active := make([]policy.ActiveBundle, 0, len(states))
	if err == nil {
		for _, state := range states {
			tenant, parseErr := domain.ParseID(state.Policy.TenantID)
			if parseErr != nil {
				err = errors.New("runtime security source returned an invalid tenant")
				break
			}
			loadedPolicy := policy.ActiveBundle{TenantID: state.Policy.TenantID, Channel: state.Policy.Channel, BundleID: state.Policy.BundleID, Digest: state.Policy.Digest, Version: state.Policy.Version, ActivatedAt: state.Policy.ActivatedAt, RefreshedAt: state.Policy.RefreshedAt}
			if setErr := r.policies.Set(loadedPolicy); setErr != nil {
				err = fmt.Errorf("load active policy metadata: %w", setErr)
				break
			}
			r.revocations.TrackTenant(tenant)
			if recordErr := r.revocations.RecordReconciliation(tenant, state.Sequence, state.Epochs); recordErr != nil {
				err = fmt.Errorf("reconcile revocation freshness: %w", recordErr)
				break
			}
			active = append(active, loadedPolicy)
		}
	}
	r.mu.Lock()
	r.lastErr = err
	if err == nil {
		r.active = active
	}
	r.mu.Unlock()
	return err
}

func (r *RuntimeSecurityReadiness) Ready(ctx context.Context) error {
	r.mu.RLock()
	lastErr := r.lastErr
	active := append([]policy.ActiveBundle(nil), r.active...)
	r.mu.RUnlock()
	if lastErr != nil {
		return fmt.Errorf("security state is not ready: %w", lastErr)
	}
	for _, item := range active {
		if _, ok := r.policies.Get(item.TenantID, item.Channel); !ok {
			return errors.New("active policy metadata is stale")
		}
	}
	revocationReadiness, err := NewRevocationReadiness(r.revocations, r.maxAge)
	if err != nil {
		return err
	}
	return revocationReadiness.Ready(ctx)
}

func (r *RuntimeSecurityReadiness) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	_ = r.Refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.Refresh(ctx)
		}
	}
}
