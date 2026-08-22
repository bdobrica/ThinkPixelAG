package domain

import (
	"testing"
	"time"
)

func TestRunAdmissionValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	ids := make([]ID, 7)
	for index := range ids {
		var err error
		ids[index], err = NewID()
		if err != nil {
			t.Fatal(err)
		}
	}
	deadline := now.Add(time.Minute)
	valid := RunAdmission{RunID: ids[0], EnvelopeID: ids[1], TenantID: ids[2], AgentID: ids[3], AgentVersionID: ids[4], RequestedBy: ids[5], PolicyDecisionID: ids[6], State: RunAdmitted, StateVersion: 1, Constraints: map[string]any{}, DeadlineAt: &deadline, CreatedAt: now, UpdatedAt: now}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid admission: %v", err)
	}
	invalid := valid
	invalid.State = RunPending
	if err := invalid.Validate(); err == nil {
		t.Fatal("pending admission accepted")
	}
	invalid = valid
	invalid.DeadlineAt = &now
	if err := invalid.Validate(); err == nil {
		t.Fatal("non-future deadline accepted")
	}
}
