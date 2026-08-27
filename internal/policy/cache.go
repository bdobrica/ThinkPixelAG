package policy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

type Evaluator interface {
	Decide(context.Context, Input) (Result, error)
}
type DecisionCache interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
}
type ActivePolicy func() (digest string, version int64, fresh bool)

type CachedEvaluator struct {
	next       Evaluator
	cache      DecisionCache
	active     ActivePolicy
	maxTTL     time.Duration
	now        func() time.Time
	mu         sync.RWMutex
	generation map[string]uint64
	local      map[string]localCacheEntry
}

const maxLocalDecisionEntries = 1024

type localCacheEntry struct {
	tenant    string
	raw       []byte
	expiresAt time.Time
}

func NewCachedEvaluator(next Evaluator, cache DecisionCache, active ActivePolicy, maxTTL time.Duration, now func() time.Time) (*CachedEvaluator, error) {
	if next == nil || active == nil || maxTTL <= 0 || now == nil {
		return nil, errors.New("cached evaluator dependencies and bounds are required")
	}
	return &CachedEvaluator{next: next, cache: cache, active: active, maxTTL: maxTTL, now: now, generation: make(map[string]uint64), local: make(map[string]localCacheEntry)}, nil
}

// InvalidateTenant advances the process-local cache namespace for tenant and
// synchronously removes its local entries. An empty tenant invalidates every
// locally known tenant for a global security change. Valkey entries use the same
// generation in their hashed key, so older entries become unreachable and are
// left only for their bounded TTL cleanup. This operation deliberately has no
// remote availability dependency.
func (e *CachedEvaluator) InvalidateTenant(tenant string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	if tenant == "" {
		e.generation[""]++
		clear(e.local)
		e.mu.Unlock()
		return
	}
	e.generation[tenant]++
	for key, entry := range e.local {
		if entry.tenant == tenant {
			delete(e.local, key)
		}
	}
	e.mu.Unlock()
}

type cacheEntry struct {
	Decision      Decision  `json:"decision"`
	PolicyDigest  string    `json:"policy_digest"`
	PolicyVersion int64     `json:"policy_version"`
	InputDigest   string    `json:"input_digest"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func (e *CachedEvaluator) Decide(ctx context.Context, in Input) (Result, error) {
	if err := in.Validate(); err != nil {
		return Result{}, err
	}
	// Authoritative operations must reach both the revocation authority and
	// policy evaluator live. Neither an ALLOW nor a DENY is reused or stored.
	if in.SecurityState.Authoritative {
		result, err := e.next.Decide(ctx, in)
		if err == nil {
			result.Metadata.CacheStatus = "bypass"
		}
		return result, err
	}
	digest, version, fresh := e.active()
	if !fresh || digest == "" || version < 1 {
		return e.next.Decide(ctx, in)
	}
	inputDigest, err := AuthorizationDigest(in)
	if err != nil {
		return Result{}, err
	}
	globalGeneration, tenantGeneration := e.cacheGeneration(in.Subject.TenantID)
	key := cacheKey(digest, version, globalGeneration, tenantGeneration, in, inputDigest)
	if raw, ok := e.localGet(key); ok {
		if hit, valid := e.decode(raw, in, digest, version, inputDigest); valid {
			return hit, nil
		}
	}
	if e.cache != nil {
		if raw, getErr := e.cache.Get(ctx, key); getErr == nil {
			if hit, ok := e.decode(raw, in, digest, version, inputDigest); ok {
				e.localSet(key, in.Subject.TenantID, raw, e.now().Add(e.ttl(hit.Decision, in)))
				return hit, nil
			}
		}
	}
	result, err := e.next.Decide(ctx, in)
	if err != nil {
		return Result{}, err
	}
	result.Metadata.CacheStatus = "miss"
	ttl := e.ttl(result.Decision, in)
	if e.cache != nil && ttl > 0 {
		entry := cacheEntry{Decision: result.Decision, PolicyDigest: result.Metadata.PolicyDigest, PolicyVersion: result.Metadata.PolicyVersion, InputDigest: inputDigest, ExpiresAt: e.now().Add(ttl)}
		entry.Decision.DecisionID = ""
		if raw, marshalErr := json.Marshal(entry); marshalErr == nil {
			e.localSet(key, in.Subject.TenantID, raw, entry.ExpiresAt)
			_ = e.cache.Set(ctx, key, raw, ttl)
		}
	} else if ttl > 0 {
		entry := cacheEntry{Decision: result.Decision, PolicyDigest: result.Metadata.PolicyDigest, PolicyVersion: result.Metadata.PolicyVersion, InputDigest: inputDigest, ExpiresAt: e.now().Add(ttl)}
		entry.Decision.DecisionID = ""
		if raw, marshalErr := json.Marshal(entry); marshalErr == nil {
			e.localSet(key, in.Subject.TenantID, raw, entry.ExpiresAt)
		}
	}
	return result, nil
}

func (e *CachedEvaluator) cacheGeneration(tenant string) (uint64, uint64) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.generation[""], e.generation[tenant]
}

func (e *CachedEvaluator) localGet(key string) ([]byte, bool) {
	e.mu.RLock()
	entry, ok := e.local[key]
	e.mu.RUnlock()
	if !ok || !e.now().Before(entry.expiresAt) {
		return nil, false
	}
	return append([]byte(nil), entry.raw...), true
}

func (e *CachedEvaluator) localSet(key, tenant string, raw []byte, expiresAt time.Time) {
	e.mu.Lock()
	if len(e.local) >= maxLocalDecisionEntries {
		for candidate := range e.local {
			delete(e.local, candidate)
			break
		}
	}
	e.local[key] = localCacheEntry{tenant: tenant, raw: append([]byte(nil), raw...), expiresAt: expiresAt}
	e.mu.Unlock()
}
func (e *CachedEvaluator) decode(raw []byte, in Input, digest string, version int64, inputDigest string) (Result, bool) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return Result{}, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var entry cacheEntry
	if dec.Decode(&entry) != nil || entry.PolicyDigest != digest || entry.PolicyVersion != version || entry.InputDigest != inputDigest || !e.now().Before(entry.ExpiresAt) {
		return Result{}, false
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return Result{}, false
	}
	entry.Decision.DecisionID = in.DecisionID
	if ValidateDecision(entry.Decision, in, e.maxTTL) != nil {
		return Result{}, false
	}
	return Result{Decision: entry.Decision, Metadata: Metadata{PolicyDigest: digest, PolicyVersion: version, InputDigest: inputDigest, CacheStatus: "hit"}}, true
}
func (e *CachedEvaluator) ttl(d Decision, in Input) time.Duration {
	ttl := time.Duration(d.DecisionTTLSeconds) * time.Second
	if ttl <= 0 {
		return 0
	}
	if ttl > e.maxTTL {
		ttl = e.maxTTL
	}
	if !in.SecurityState.Authoritative {
		bound := in.SecurityState.FreshnessMaxAgeSeconds
		if bound <= 0 {
			return 0
		}
		remaining := time.Duration(bound-in.SecurityState.AgeSeconds) * time.Second
		if remaining <= 0 {
			return 0
		}
		if ttl > remaining {
			ttl = remaining
		}
	}
	return ttl
}

// CacheKey hashes all components so Valkey keys do not expose tenant,
// principal, action, or resource identifiers to operational tooling.
func CacheKey(policyDigest string, version int64, in Input, inputDigest string) string {
	return cacheKey(policyDigest, version, 0, 0, in, inputDigest)
}

func cacheKey(policyDigest string, version int64, globalGeneration, tenantGeneration uint64, in Input, inputDigest string) string {
	material := fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s", ContractVersion, policyDigest, version, globalGeneration, tenantGeneration, in.SecurityState.GlobalEpoch, in.SecurityState.TenantPolicyEpoch, in.SecurityState.TenantRevocationEpoch, in.SecurityState.AgentRevocationEpoch, in.SecurityState.FreshnessMaxAgeSeconds, in.Subject.TenantID, in.Subject.PrincipalID, in.Action, in.Resource.Type+":"+in.Resource.ID, inputDigest)
	sum := sha256.Sum256([]byte(material))
	return "thinkpixelag:policy:v1:" + hex.EncodeToString(sum[:])
}
