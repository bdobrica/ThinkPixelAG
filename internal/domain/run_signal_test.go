package domain

import (
	"testing"
	"time"
)

func TestRunSignalValidatesTypedPayloads(t *testing.T) {
	id := mustID(t)
	base := RunSignal{ID: id, TenantID: id, RunID: id, ActorPrincipalID: id, IdempotencyKey: "abcdefghijklmnop", CreatedAt: time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)}
	tests := []struct {
		name    string
		kind    RunSignalType
		payload string
		valid   bool
	}{
		{"pause", RunSignalPause, `{"reason_code":"operator.request"}`, true}, {"resume", RunSignalResume, `{}`, true}, {"custom", RunSignalCustom, `{"name":"runtime.refresh","data":{"scope":"tools"}}`, true},
		{"cancel reserved", "CANCEL", `{}`, false}, {"resume fields", RunSignalResume, `{"x":1}`, false}, {"custom missing data", RunSignalCustom, `{"name":"refresh"}`, false}, {"oversized name", RunSignalCustom, `{"name":"bad name","data":{}}`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signal := base
			signal.Type = test.kind
			signal.Payload = []byte(test.payload)
			if got := signal.Validate() == nil; got != test.valid {
				t.Fatalf("valid=%v error=%v", got, signal.Validate())
			}
		})
	}
}
