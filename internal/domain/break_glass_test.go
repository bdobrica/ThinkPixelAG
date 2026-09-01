package domain

import (
	"strings"
	"testing"
	"time"
)

func TestBreakGlassGrantIsNarrowAndTimeBound(t *testing.T) {
	ids := make([]ID, 4)
	for i := range ids {
		ids[i], _ = NewID()
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	expires := now.Add(15 * time.Minute)
	digest, err := BreakGlassGrantDigest(ids[1], ids[2], BreakGlassPolicyRecovery, "policy_channel", "stable", "security.recovery", expires)
	if err != nil {
		t.Fatal(err)
	}
	grant := BreakGlassGrant{ID: ids[0], TenantID: ids[1], PrincipalID: ids[2], ApprovalID: ids[3], Scope: BreakGlassPolicyRecovery, ResourceType: "policy_channel", ResourceID: "stable", ReasonCode: "security.recovery", GrantDigest: digest, CredentialDigest: DigestBreakGlassCredential("secret"), StrongAuthenticationReference: "webauthn:assertion-1", IssuedAt: now, ExpiresAt: expires}
	if err := grant.Validate(); err != nil {
		t.Fatal(err)
	}
	grant.ExpiresAt = now.Add(15*time.Minute + time.Nanosecond)
	if err := grant.Validate(); err == nil {
		t.Fatal("grant longer than fifteen minutes accepted")
	}
	grant.ExpiresAt = expires
	grant.Scope = "ALL"
	if err := grant.Validate(); err == nil {
		t.Fatal("unknown broad scope accepted")
	}
	if DigestBreakGlassCredential("secret") == DigestBreakGlassCredential("other") || strings.Contains(DigestBreakGlassCredential("secret"), "secret") {
		t.Fatal("credential digest is not one-way and binding")
	}
}
