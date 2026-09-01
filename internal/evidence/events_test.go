package evidence

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

func TestMachineReadableSchemaTracksClosedContract(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../api/schemas/evidence-event-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	for _, required := range []string{ContractVersion, string(Policy), string(Revocation), string(ResourceOverride), string(Approval), string(KeyLifecycle), string(VersionApproval), string(BreakGlass)} {
		if !strings.Contains(encoded, `"`+required+`"`) {
			t.Fatalf("schema does not contain %q", required)
		}
	}
	if schema["additionalProperties"] != false {
		t.Fatal("top-level evidence schema is not closed")
	}
}

func TestClosedEvidenceSchemas(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	event := validEvent(t, now)
	epoch := int64(2)
	tests := []struct {
		kind Type
		data any
	}{
		{Policy, ArtifactData{ArtifactID: "policy-7", ArtifactKind: "POLICY_BUNDLE", FormatVersion: "thinkpixelag.authorization/v1alpha1", Revision: 7, PayloadDigest: digest, KeyID: "kms/key/policy", KeyVersion: "4", SignatureAlgorithm: "ECDSA_SHA256"}},
		{ResourceOverride, ArtifactData{ArtifactID: "override-2", ArtifactKind: "RESOURCE_OVERRIDE_CONFIG", FormatVersion: "thinkpixelag.resource-overrides/v1", Revision: 2, PayloadDigest: digest, KeyID: "hsm/key/resource", KeyVersion: "9", SignatureAlgorithm: "ED25519", ApprovalID: "approval-1"}},
		{Revocation, RevocationData{RevocationID: "revocation-1", Change: "CREATED", Scope: "tenant_id", Target: "tenant-1", SecurityEpoch: 4, TenantRevocationEpoch: &epoch}},
		{Approval, ApprovalData{ApprovalID: "approval-1", ActionClass: "POLICY_ROLLBACK", ResourceType: "policy", ResourceID: "stable", RequestDigest: digest, State: "APPROVED", RequesterID: "requester-1", ApproverID: "approver-2", ExpiresAt: now.Add(time.Hour)}},
		{KeyLifecycle, KeyLifecycleData{KeyID: "kms/key/policy", KeyVersion: "5", Algorithm: "ECDSA_SHA256", Protection: "KMS", Change: "ROTATED", PriorKeyVersion: "4", ApprovalID: "approval-2"}},
		{VersionApproval, VersionApprovalData{ApprovalID: "version-approval-1", AgentID: "agent-1", AgentVersionID: "version-1", ContentDigest: digest, Decision: "APPROVED"}},
		{BreakGlass, BreakGlassData{SessionID: "session-1", Scope: "revocations.global", GrantDigest: digest, ApprovalID: "approval-3", ExpiresAt: now.Add(15 * time.Minute), Change: "ACTIVATED"}},
	}
	for _, test := range tests {
		test := test
		t.Run(string(test.kind), func(t *testing.T) {
			event.EventType = test.kind
			got, err := New(event, test.data)
			if err != nil {
				t.Fatal(err)
			}
			if got.SchemaVersion != ContractVersion || got.Validate() != nil {
				t.Fatalf("invalid event: %+v", got)
			}
			encoded, err := json.Marshal(got)
			if err != nil || strings.Contains(string(encoded), "private") {
				t.Fatalf("encoded=%s err=%v", encoded, err)
			}
		})
	}
}

func TestEvidenceRejectsSubstitutionMalformedAndUnboundedData(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	base := validEvent(t, now)
	base.EventType = Policy
	valid, err := New(base, ArtifactData{ArtifactID: "policy", ArtifactKind: "POLICY_BUNDLE", FormatVersion: "v1", Revision: 1, PayloadDigest: digest, KeyID: "key", KeyVersion: "1", SignatureAlgorithm: "ED25519"})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]func(*Event){
		"version":           func(v *Event) { v.SchemaVersion = "v2" },
		"kind substitution": func(v *Event) { v.EventType = ResourceOverride },
		"unknown field":     func(v *Event) { v.Data = append(v.Data[:len(v.Data)-1], []byte(`,"secret":"private"}`)...) },
		"digest": func(v *Event) {
			v.Data = []byte(`{"artifact_id":"policy","artifact_kind":"POLICY_BUNDLE","format_version":"v1","revision":1,"payload_digest":"sha256:no","key_id":"key","key_version":"1","signature_algorithm":"ED25519"}`)
		},
		"reasons": func(v *Event) { v.ReasonCodes = nil },
		"trace":   func(v *Event) { v.TraceID = "trace" },
		"time":    func(v *Event) { v.OccurredAt = v.OccurredAt.In(time.FixedZone("other", 3600)) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if candidate.Validate() == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
	base.EventType = Approval
	if _, err := New(base, ApprovalData{ApprovalID: "approval", ActionClass: "POLICY_ROLLBACK", ResourceType: "policy", ResourceID: "stable", RequestDigest: digest, State: "APPROVED", RequesterID: "same", ApproverID: "same", ExpiresAt: now.Add(time.Hour)}); err == nil {
		t.Fatal("self-approved evidence accepted")
	}
}

func validEvent(t *testing.T, now time.Time) Event {
	t.Helper()
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return Event{ID: id, Actor: Actor{Type: "PRINCIPAL", ID: "principal-1"}, Action: "policy.activate", Outcome: Succeeded, ReasonCodes: []string{"policy.allowed"}, OccurredAt: now}
}
