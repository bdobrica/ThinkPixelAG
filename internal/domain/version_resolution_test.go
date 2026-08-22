package domain

import (
	"strings"
	"testing"
	"time"
)

func TestRunVersionResolutionRequiresSelectionEvidenceForControlledModes(t *testing.T) {
	identifier, _ := NewID()
	resolution := RunVersionResolution{RunID: identifier, TenantID: identifier, AgentID: identifier, AgentVersionID: identifier, ApprovalID: identifier,
		AgentContentDigest: "sha256:" + strings.Repeat("a", 64), PolicyBundleDigest: "sha256:" + strings.Repeat("b", 64), PolicyActivationVersion: 1,
		Mode: ResolutionAutomatic, InvocationDecisionID: identifier, ResolvedConstraints: map[string]any{}, ResolvedAt: time.Now().UTC()}
	if err := resolution.Validate(); err != nil {
		t.Fatal(err)
	}
	resolution.Mode = ResolutionPinned
	if err := resolution.Validate(); err == nil {
		t.Fatal("controlled pin accepted without selection policy evidence")
	}
	resolution.SelectionDecisionID = identifier
	if err := resolution.Validate(); err != nil {
		t.Fatal(err)
	}
}
