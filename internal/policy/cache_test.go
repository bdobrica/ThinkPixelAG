package policy

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeEvaluator struct {
	calls    int
	decision Decision
	err      error
}

func (f *fakeEvaluator) Decide(_ context.Context, in Input) (Result, error) {
	f.calls++
	d := f.decision
	d.DecisionID = in.DecisionID
	return Result{Decision: d, Metadata: Metadata{PolicyDigest: "sha256:policy", PolicyVersion: 7}}, f.err
}

type memoryCache struct {
	values         map[string][]byte
	getErr, setErr error
	ttl            time.Duration
	sets           int
}

func (c *memoryCache) Get(_ context.Context, key string) ([]byte, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	v, ok := c.values[key]
	if !ok {
		return nil, errors.New("miss")
	}
	return append([]byte(nil), v...), nil
}
func (c *memoryCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	c.sets++
	c.ttl = ttl
	if c.setErr != nil {
		return c.setErr
	}
	c.values[key] = append([]byte(nil), value...)
	return nil
}
func cacheInput(id string) Input {
	in := validInput()
	in.DecisionID = id
	in.Context.RequestID = "request-" + id
	in.RequestTime = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	in.SecurityState = SecurityState{GlobalEpoch: 1, TenantPolicyEpoch: 2, TenantRevocationEpoch: 3, AgentRevocationEpoch: 4, AgeSeconds: 4, FreshnessMaxAgeSeconds: 30}
	return in
}
func allowDecision() Decision {
	return Decision{ContractVersion: ContractVersion, Allow: true, ReasonCodes: []string{"agent.invoke.allowed"}, ResolvedConstraints: map[string]any{}, Obligations: []Obligation{}, DecisionTTLSeconds: 10}
}

func TestCachedEvaluatorHitsAcrossEvidenceFieldsAndRebindsDecision(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	cache := &memoryCache{values: map[string][]byte{}}
	next := &fakeEvaluator{decision: allowDecision()}
	e, err := NewCachedEvaluator(next, cache, func() (string, int64, bool) { return "sha256:policy", 7, true }, 30*time.Second, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.Decide(context.Background(), cacheInput("first"))
	if err != nil {
		t.Fatal(err)
	}
	secondIn := cacheInput("second")
	secondIn.RequestTime = secondIn.RequestTime.Add(time.Second)
	second, err := e.Decide(context.Background(), secondIn)
	if err != nil {
		t.Fatal(err)
	}
	if next.calls != 1 || first.Metadata.CacheStatus != "miss" || second.Metadata.CacheStatus != "hit" || second.Decision.DecisionID != "second" {
		t.Fatalf("calls=%d first=%#v second=%#v", next.calls, first, second)
	}
	if cache.ttl != 10*time.Second {
		t.Fatalf("ttl=%s", cache.ttl)
	}
}
func TestCachedEvaluatorEpochAndPolicyChangesMiss(t *testing.T) {
	now := time.Now().UTC()
	cache := &memoryCache{values: map[string][]byte{}}
	next := &fakeEvaluator{decision: allowDecision()}
	version := int64(7)
	e, _ := NewCachedEvaluator(next, cache, func() (string, int64, bool) { return "sha256:policy", version, true }, 30*time.Second, func() time.Time { return now })
	in := cacheInput("one")
	_, _ = e.Decide(context.Background(), in)
	in.DecisionID = "two"
	in.SecurityState.GlobalEpoch++
	_, _ = e.Decide(context.Background(), in)
	version++
	in.DecisionID = "three"
	_, _ = e.Decide(context.Background(), in)
	if next.calls != 3 {
		t.Fatalf("authoritative calls=%d", next.calls)
	}
}
func TestCachedEvaluatorBypassesFailureAndPoison(t *testing.T) {
	now := time.Now().UTC()
	cache := &memoryCache{values: map[string][]byte{}, getErr: errors.New("down"), setErr: errors.New("down")}
	next := &fakeEvaluator{decision: allowDecision()}
	e, _ := NewCachedEvaluator(next, cache, func() (string, int64, bool) { return "sha256:policy", 7, true }, 30*time.Second, func() time.Time { return now })
	if _, err := e.Decide(context.Background(), cacheInput("one")); err != nil {
		t.Fatal(err)
	}
	cache.getErr = nil
	in := cacheInput("two")
	digest, _ := AuthorizationDigest(in)
	cache.values[CacheKey("sha256:policy", 7, in, digest)] = []byte(`{"decision":{"allow":true}}`)
	if _, err := e.Decide(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if next.calls != 2 {
		t.Fatalf("poisoned cache avoided evaluator: calls=%d", next.calls)
	}
}
func TestCachedEvaluatorBoundsFreshnessAndZeroTTL(t *testing.T) {
	now := time.Now().UTC()
	cache := &memoryCache{values: map[string][]byte{}}
	decision := allowDecision()
	decision.DecisionTTLSeconds = 20
	next := &fakeEvaluator{decision: decision}
	e, _ := NewCachedEvaluator(next, cache, func() (string, int64, bool) { return "sha256:policy", 7, true }, 30*time.Second, func() time.Time { return now })
	in := cacheInput("one")
	in.SecurityState.AgeSeconds = 29
	if _, err := e.Decide(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if cache.ttl != time.Second {
		t.Fatalf("freshness TTL=%s", cache.ttl)
	}
	decision.DecisionTTLSeconds = 0
	next.decision = decision
	in.DecisionID = "zero"
	in.Action = "resources.settle"
	before := cache.sets
	if _, err := e.Decide(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if cache.sets != before {
		t.Fatal("cached zero-TTL decision")
	}
}

func TestCachedEvaluatorBypassesAuthoritativeOperations(t *testing.T) {
	now := time.Now().UTC()
	cache := &memoryCache{values: map[string][]byte{}}
	next := &fakeEvaluator{decision: allowDecision()}
	e, _ := NewCachedEvaluator(next, cache, func() (string, int64, bool) { return "sha256:policy", 7, true }, time.Minute, func() time.Time { return now })
	in := cacheInput("live-one")
	in.SecurityState.Authoritative = true
	first, err := e.Decide(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.DecisionID = "live-two"
	second, err := e.Decide(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if next.calls != 2 || cache.sets != 0 || first.Metadata.CacheStatus != "bypass" || second.Metadata.CacheStatus != "bypass" {
		t.Fatalf("authoritative cache use: calls=%d sets=%d first=%+v second=%+v", next.calls, cache.sets, first.Metadata, second.Metadata)
	}
}

func TestCachedEvaluatorUsesOperationFreshnessBudget(t *testing.T) {
	now := time.Now().UTC()
	cache := &memoryCache{values: map[string][]byte{}}
	decision := allowDecision()
	decision.DecisionTTLSeconds = 20
	e, _ := NewCachedEvaluator(&fakeEvaluator{decision: decision}, cache, func() (string, int64, bool) { return "sha256:policy", 7, true }, time.Minute, func() time.Time { return now })
	in := cacheInput("sensitive")
	in.SecurityState.AgeSeconds = 50
	in.SecurityState.FreshnessMaxAgeSeconds = 60
	if _, err := e.Decide(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if cache.ttl != 10*time.Second {
		t.Fatalf("sensitive-read freshness TTL=%s", cache.ttl)
	}
}
func TestAuthorizationDigestNormalizesSets(t *testing.T) {
	a, b := cacheInput("a"), cacheInput("b")
	a.Subject.Roles = []string{"z", "a", "a"}
	b.Subject.Roles = []string{"a", "z"}
	a.Subject.AuthenticationMethods = []string{"mfa", "oidc"}
	b.Subject.AuthenticationMethods = []string{"oidc", "mfa"}
	da, _ := AuthorizationDigest(a)
	db, _ := AuthorizationDigest(b)
	if da != db {
		t.Fatalf("equivalent input digests differ: %s %s", da, db)
	}
}

func TestCacheKeyBindsEveryAuthorizationDimension(t *testing.T) {
	base := cacheInput("base")
	digest, _ := AuthorizationDigest(base)
	original := CacheKey("sha256:policy", 7, base, digest)
	tests := []struct {
		name    string
		mutate  func(*Input)
		policy  string
		version int64
	}{
		{"policy digest", func(*Input) {}, "sha256:other", 7},
		{"policy version", func(*Input) {}, "sha256:policy", 8},
		{"global epoch", func(in *Input) { in.SecurityState.GlobalEpoch++ }, "sha256:policy", 7},
		{"tenant epoch", func(in *Input) { in.SecurityState.TenantPolicyEpoch++ }, "sha256:policy", 7},
		{"tenant revocation epoch", func(in *Input) { in.SecurityState.TenantRevocationEpoch++ }, "sha256:policy", 7},
		{"agent epoch", func(in *Input) { in.SecurityState.AgentRevocationEpoch++ }, "sha256:policy", 7},
		{"freshness bound", func(in *Input) { in.SecurityState.FreshnessMaxAgeSeconds++ }, "sha256:policy", 7},
		{"subject", func(in *Input) { in.Subject.PrincipalID = "other" }, "sha256:policy", 7},
		{"action", func(in *Input) { in.Action = "runs.cancel" }, "sha256:policy", 7},
		{"resource", func(in *Input) { in.Resource.ID = "other" }, "sha256:policy", 7},
		{"input digest", func(in *Input) { in.AuthorityConstraints = map[string]any{"max_tokens": float64(1)} }, "sha256:policy", 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base
			tt.mutate(&in)
			changedDigest, _ := AuthorizationDigest(in)
			if got := CacheKey(tt.policy, tt.version, in, changedDigest); got == original {
				t.Fatal("cache key did not change")
			}
		})
	}
}
