package domain

import (
	"testing"
	"time"
)

func TestGovernanceApprovalEnforcesFourEyesAndSingleUse(t *testing.T) {
	now := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	requester, _ := ParseID("01990a20-0000-7000-8000-000000000001")
	approver, _ := ParseID("01990a20-0000-7000-8000-000000000002")
	tenant, _ := ParseID("01990a20-0000-7000-8000-000000000003")
	id, _ := ParseID("01990a20-0000-7000-8000-000000000004")
	request := GovernanceApproval{ID: id, TenantID: tenant, RequesterPrincipalID: requester, Action: ApprovalPolicyRollback,
		ResourceType: "policy", ResourceID: "production", RequestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReasonCode: "incident.rollback", Provider: "test", ProviderReference: "provider-1", State: GovernanceApprovalPending,
		RequestedAt: now, ExpiresAt: now.Add(time.Hour)}
	if _, err := request.Decide(requester, true, "decision-1", now.Add(time.Minute)); err == nil {
		t.Fatal("requester approved their own action")
	}
	approved, err := request.Decide(approver, true, "decision-1", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := approved.Consume(request.RequestDigest, now.Add(2*time.Minute))
	if err != nil || consumed.State != GovernanceApprovalConsumed {
		t.Fatalf("consume approval: state=%s err=%v", consumed.State, err)
	}
	if _, err := consumed.Consume(request.RequestDigest, now.Add(3*time.Minute)); err == nil {
		t.Fatal("approval was consumed twice")
	}
}

func TestGovernanceApprovalRejectsExpiryAndDigestMismatch(t *testing.T) {
	now := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
	requester, _ := ParseID("01990a20-0000-7000-8000-000000000001")
	approver, _ := ParseID("01990a20-0000-7000-8000-000000000002")
	tenant, _ := ParseID("01990a20-0000-7000-8000-000000000003")
	id, _ := ParseID("01990a20-0000-7000-8000-000000000004")
	request := GovernanceApproval{ID: id, TenantID: tenant, RequesterPrincipalID: requester, Action: ApprovalTrustRootRotation,
		ResourceType: "trust_root", ResourceID: "primary", RequestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReasonCode: "rotation.scheduled", Provider: "test", ProviderReference: "provider-1", State: GovernanceApprovalPending,
		RequestedAt: now, ExpiresAt: now.Add(time.Hour)}
	if state := request.EffectiveState(request.ExpiresAt); state != GovernanceApprovalExpired {
		t.Fatalf("effective state at expiry = %s", state)
	}
	if _, err := request.Decide(approver, true, "decision-1", request.ExpiresAt); err == nil {
		t.Fatal("accepted decision at expiry")
	}
	approved, _ := request.Decide(approver, true, "decision-1", now.Add(time.Minute))
	if _, err := approved.Consume("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", now.Add(2*time.Minute)); err == nil {
		t.Fatal("accepted mismatched action digest")
	}
}
