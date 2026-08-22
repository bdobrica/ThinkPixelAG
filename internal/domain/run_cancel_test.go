package domain

import (
	"testing"
	"time"
)

func TestRunCancellationValidation(t *testing.T) {
	id, _ := NewID()
	expected := int64(3)
	valid := RunCancellation{TenantID: id, RunID: id, ActorPrincipalID: id, IdempotencyKey: "cancel:abcdefghijklmnop", ReasonCode: "caller.request", ExpectedStateVersion: &expected, CreatedAt: time.Date(2026, 8, 22, 17, 0, 0, 0, time.UTC)}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*RunCancellation){
		"bad reason":  func(value *RunCancellation) { value.ReasonCode = "contains spaces" },
		"bad version": func(value *RunCancellation) { zero := int64(0); value.ExpectedStateVersion = &zero },
		"bad time":    func(value *RunCancellation) { value.CreatedAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if candidate.Validate() == nil {
				t.Fatal("invalid cancellation passed validation")
			}
		})
	}
}
