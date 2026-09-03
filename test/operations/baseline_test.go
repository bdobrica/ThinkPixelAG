package operations_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
)

// These benchmarks are deterministic component baselines. They deliberately do
// not assert production SLOs: cluster qualification includes network, database,
// OPA, cache, and scheduler behavior that an in-process benchmark cannot model.

func BenchmarkPolicyContract(b *testing.B) {
	input := policy.Input{
		ContractVersion: policy.ContractVersion,
		DecisionID:      "decision-load-baseline",
		RequestTime:     time.Unix(1_788_000_000, 0).UTC(),
		Subject: policy.Subject{
			PrincipalID: "principal-load", TenantID: "tenant-load",
			PrincipalType: "human", Roles: []string{"developer"}, Issuer: "https://issuer.example",
		},
		Action: "agents.invoke",
		Resource: policy.Resource{
			Type: "agent", ID: "agent-load", TenantID: "tenant-load",
			Attributes: map[string]any{"risk_class": "medium"},
		},
		RequestedConstraints: map[string]any{"max_llm_tokens": float64(2_000)},
		AuthorityConstraints: map[string]any{"max_llm_tokens": float64(4_000)},
		SecurityState: policy.SecurityState{
			GlobalEpoch: 1, TenantPolicyEpoch: 1, TenantRevocationEpoch: 1,
			FreshnessMaxAgeSeconds: 30,
		},
		Context: policy.RequestContext{RequestID: "request-load"},
	}
	decision := policy.Decision{
		ContractVersion: policy.ContractVersion, DecisionID: input.DecisionID,
		Allow: true, ReasonCodes: []string{"agent.invoke.allowed"},
		ResolvedConstraints: map[string]any{"max_llm_tokens": float64(2_000)},
		Obligations:         []policy.Obligation{}, DecisionTTLSeconds: 10,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := input.Validate(); err != nil {
			b.Fatal(err)
		}
		if err := policy.ValidateDecision(decision, input, 30*time.Second); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunEventCursor(b *testing.B) {
	run := mustID(b)
	codec, err := domain.NewRunEventCursorCodec([]byte("load-baseline-run-event-cursor-key"))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		encoded, encodeErr := codec.Encode(run, 42)
		if encodeErr != nil {
			b.Fatal(encodeErr)
		}
		if sequence, decodeErr := codec.Decode(encoded, run); decodeErr != nil || sequence != 42 {
			b.Fatalf("cursor round trip: sequence=%d err=%v", sequence, decodeErr)
		}
	}
}

func BenchmarkRevocationFanoutReceipt(b *testing.B) {
	tracker, err := application.NewRevocationFreshnessTracker(domain.SystemClock{})
	if err != nil {
		b.Fatal(err)
	}
	tenants := make([]domain.ID, 5_000)
	for i := range tenants {
		tenants[i] = mustID(b)
		tracker.TrackTenant(tenants[i])
	}
	epochs := domain.EpochVector{Security: 1, TenantRevocation: 1}
	b.ReportMetric(float64(len(tenants)), "clients")
	b.ResetTimer()
	for i := range b.N {
		tenant := tenants[i%len(tenants)]
		if err := tracker.RecordStreamReceipt(tenant, int64(i/len(tenants)+1), epochs); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRevocationCursor(b *testing.B) {
	tenant := mustID(b)
	codec, err := domain.NewRevocationCursorCodec([]byte("load-baseline-revocation-cursor-key"))
	if err != nil {
		b.Fatal(err)
	}
	cursor := domain.RevocationCursor{Sequence: 42, SecurityEpoch: 7}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		encoded, encodeErr := codec.Encode(tenant, cursor)
		if encodeErr != nil {
			b.Fatal(encodeErr)
		}
		decoded, decodeErr := codec.Decode(encoded, tenant)
		if decodeErr != nil || decoded.Sequence != cursor.Sequence || decoded.SecurityEpoch != cursor.SecurityEpoch {
			b.Fatalf("cursor round trip: decoded=%+v err=%v", decoded, decodeErr)
		}
	}
}

func mustID(tb testing.TB) domain.ID {
	tb.Helper()
	id, err := domain.NewID()
	if err != nil {
		tb.Fatal(fmt.Errorf("generate benchmark identifier: %w", err))
	}
	return id
}
