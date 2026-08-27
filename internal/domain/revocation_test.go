package domain

import (
	"testing"
	"time"
)

func TestRevocationValidationAllScopesAndTimeBounds(t *testing.T) {
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	actor, _ := NewID()
	tenant, _ := NewID()
	for _, scope := range []RevocationScope{RevocationRunID, RevocationAgentID, RevocationAgentVersion, RevocationSkillDigest, RevocationPrincipalID, RevocationTenantID, RevocationToolID, RevocationPolicyVersion} {
		id, _ := NewID()
		expires := now.Add(time.Hour)
		if _, err := ValidateRevocation(Revocation{ID: id, TenantID: &tenant, ActorPrincipalID: actor, Scope: scope, Target: "target", ReasonCode: "security.compromise", EffectiveAt: now, ExpiresAt: &expires, CreatedAt: now}); err != nil {
			t.Errorf("scope %s rejected: %v", scope, err)
		}
	}
	id, _ := NewID()
	if _, err := ValidateRevocation(Revocation{ID: id, ActorPrincipalID: actor, Scope: RevocationGlobal, Target: "all", ReasonCode: "security.emergency", EffectiveAt: now, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateRevocation(Revocation{ID: id, TenantID: &tenant, ActorPrincipalID: actor, Scope: RevocationGlobal, Target: "all", ReasonCode: "bad reason", EffectiveAt: now, CreatedAt: now}); err == nil {
		t.Fatal("accepted invalid global tenant/reason")
	}
}
