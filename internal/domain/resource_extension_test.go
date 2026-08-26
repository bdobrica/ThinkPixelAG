package domain

import (
	"testing"
	"time"
)

func TestResourceExtensionValidation(t *testing.T) {
	ids := []ID{mustID(t), mustID(t), mustID(t), mustID(t), mustID(t)}
	amount, _ := NewDecimal(5, 0)
	q, _ := NewQuantity(amount, "llm_tokens")
	extension := ResourceExtension{ID: ids[0], TenantID: ids[1], RunID: ids[2], ActorPrincipalID: ids[3], PolicyDecisionID: ids[4], IdempotencyKey: "extension-key", ReasonCode: "budget_increase", ApprovalReference: "CAB-42", Additions: []ResourceExtensionAmount{{Name: "llm_tokens", Quantity: q}}, CreatedAt: time.Unix(10, 0).UTC()}
	if _, err := ValidateResourceExtension(extension); err != nil {
		t.Fatal(err)
	}
	extension.ActorPrincipalID = ID{}
	if _, err := ValidateResourceExtension(extension); err == nil {
		t.Fatal("self/unknown actor-shaped extension accepted")
	}
}
