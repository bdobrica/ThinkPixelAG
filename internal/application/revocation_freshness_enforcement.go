package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

const (
	DefaultNormalWriteFreshness   = 30 * time.Second
	DefaultSensitiveReadFreshness = 60 * time.Second
)

type FreshnessClass string

const (
	FreshnessHighRiskWrite FreshnessClass = "HIGH_RISK_WRITE"
	FreshnessNormalWrite   FreshnessClass = "NORMAL_WRITE"
	FreshnessSensitiveRead FreshnessClass = "SENSITIVE_READ"
	FreshnessLowRiskRead   FreshnessClass = "LOW_RISK_READ"
)

// RevocationFreshnessPolicy may tighten the normal-write and sensitive-read
// defaults. LowRiskReadMaxAge is deliberately explicit and always bounded.
type RevocationFreshnessPolicy struct {
	NormalWriteMaxAge   time.Duration
	SensitiveReadMaxAge time.Duration
	LowRiskReadMaxAge   time.Duration
}

// FreshnessEnforcingEvaluator is the security boundary ahead of the decision
// cache. It replaces caller-provided security state with process-local state;
// high-risk writes request a live authoritative path and cannot use the cache.
type FreshnessEnforcingEvaluator struct {
	next    policy.Evaluator
	tracker *RevocationFreshnessTracker
	bounds  RevocationFreshnessPolicy
}

func NewFreshnessEnforcingEvaluator(next policy.Evaluator, tracker *RevocationFreshnessTracker, bounds RevocationFreshnessPolicy) (*FreshnessEnforcingEvaluator, error) {
	if next == nil || tracker == nil || bounds.NormalWriteMaxAge <= 0 || bounds.NormalWriteMaxAge > DefaultNormalWriteFreshness || bounds.SensitiveReadMaxAge <= 0 || bounds.SensitiveReadMaxAge > DefaultSensitiveReadFreshness || bounds.LowRiskReadMaxAge <= 0 || bounds.NormalWriteMaxAge%time.Second != 0 || bounds.SensitiveReadMaxAge%time.Second != 0 || bounds.LowRiskReadMaxAge%time.Second != 0 {
		return nil, errors.New("revocation freshness evaluator requires safe bounded configuration")
	}
	return &FreshnessEnforcingEvaluator{next: next, tracker: tracker, bounds: bounds}, nil
}

func DefaultRevocationFreshnessPolicy(lowRiskReadMaxAge time.Duration) RevocationFreshnessPolicy {
	return RevocationFreshnessPolicy{NormalWriteMaxAge: DefaultNormalWriteFreshness, SensitiveReadMaxAge: DefaultSensitiveReadFreshness, LowRiskReadMaxAge: lowRiskReadMaxAge}
}

func (e *FreshnessEnforcingEvaluator) Decide(ctx context.Context, in policy.Input) (policy.Result, error) {
	tenant, err := domain.ParseID(in.Subject.TenantID)
	if err != nil {
		return policy.Result{}, domain.NewError(domain.CodeUnavailable, "revocation freshness input is invalid").WithRetryable()
	}
	class := ClassifyFreshness(in)
	e.tracker.TrackTenant(tenant)
	if class == FreshnessHighRiskWrite {
		in.SecurityState = policy.SecurityState{Authoritative: true}
		result, err := e.next.Decide(ctx, in)
		if err == nil && result.Decision.Allow && result.Decision.DecisionTTLSeconds != 0 {
			return policy.Result{}, domain.NewError(domain.CodeUnavailable, "authoritative policy decision is not cache safe").WithRetryable()
		}
		return result, err
	}

	state := e.tracker.Snapshot(tenant)
	maxAge := e.maxAge(class)
	if !state.HasAuthoritativeState || !state.MonotonicClockHealthy || state.Age > maxAge {
		return freshnessDenied(in, "security_state.stale"), nil
	}
	// Round age upward so second-granularity policy/cache input cannot extend
	// the monotonic deadline by truncating a fractional second.
	ageSeconds := int64((state.Age + time.Second - 1) / time.Second)
	in.SecurityState = policy.SecurityState{
		GlobalEpoch: state.LastAppliedEpochs.Security, TenantPolicyEpoch: state.LastAppliedEpochs.TenantPolicy,
		TenantRevocationEpoch: state.LastAppliedEpochs.TenantRevocation, AgentRevocationEpoch: state.LastAppliedEpochs.AgentRevocation,
		AgeSeconds: ageSeconds, FreshnessMaxAgeSeconds: int64(maxAge / time.Second),
	}
	return e.next.Decide(ctx, in)
}

func (e *FreshnessEnforcingEvaluator) maxAge(class FreshnessClass) time.Duration {
	switch class {
	case FreshnessNormalWrite:
		return e.bounds.NormalWriteMaxAge
	case FreshnessSensitiveRead:
		return e.bounds.SensitiveReadMaxAge
	default:
		return e.bounds.LowRiskReadMaxAge
	}
}

func ClassifyFreshness(in policy.Input) FreshnessClass {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	switch action {
	case "agents.list":
		return FreshnessLowRiskRead
	case "agents.describe", "runs.read", "runs.events.read":
		return FreshnessSensitiveRead
	case "runs.signal", "runs.cancel":
		return FreshnessNormalWrite
	case "runs.create":
		if in.Agent != nil && (strings.EqualFold(in.Agent.RiskClass, string(domain.AgentRiskLow)) || strings.EqualFold(in.Agent.RiskClass, string(domain.AgentRiskMedium))) {
			return FreshnessNormalWrite
		}
		return FreshnessHighRiskWrite
	case "runs.claim", "runs.heartbeat", "runs.complete":
		if in.Agent != nil && (strings.EqualFold(in.Agent.RiskClass, string(domain.AgentRiskLow)) || strings.EqualFold(in.Agent.RiskClass, string(domain.AgentRiskMedium))) {
			return FreshnessNormalWrite
		}
		return FreshnessHighRiskWrite
	default:
		// Unknown and administrative/resource actions take the conservative
		// live path. New writes cannot silently inherit a cacheable class.
		return FreshnessHighRiskWrite
	}
}

func freshnessDenied(in policy.Input, reason string) policy.Result {
	return policy.Result{Decision: policy.Decision{ContractVersion: policy.ContractVersion, DecisionID: in.DecisionID, Allow: false, ReasonCodes: []string{reason}, ResolvedConstraints: map[string]any{}, Obligations: []policy.Obligation{}, DecisionTTLSeconds: 0}}
}
