package opa

import (
	"context"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func input() policy.Input {
	return policy.Input{ContractVersion: policy.ContractVersion, DecisionID: "d", RequestTime: time.Now().UTC(), Subject: policy.Subject{PrincipalID: "p", TenantID: "t", PrincipalType: "human"}, Action: "agents.list", Resource: policy.Resource{Type: "catalog", TenantID: "t", Attributes: map[string]any{}}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: policy.SecurityState{}, Context: policy.RequestContext{RequestID: "r"}}
}
func TestClientValidatesResponseAndMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("token missing")
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"contract_version": policy.ContractVersion, "decision_id": "d", "allow": true, "reason_codes": []string{"agent.discover.allowed"}, "resolved_constraints": map[string]any{}, "obligations": []any{}, "decision_ttl_seconds": 1}})
	}))
	defer srv.Close()
	c, err := New(srv.URL, "/decision", time.Second, time.Second, "secret", nil, func() (string, int64, bool) { return "sha256:x", 2, true })
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Decide(context.Background(), input())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Decision.Allow || got.Metadata.PolicyVersion != 2 {
		t.Fatalf("unexpected result: %#v", got)
	}
}
func TestClientFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		delay      time.Duration
	}{{"malformed", `{"result":{"allow":true}}`, 0}, {"timeout", `{}`, 50 * time.Millisecond}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(tc.delay); w.Write([]byte(tc.body)) }))
			defer srv.Close()
			c, _ := New(srv.URL, "/decision", 10*time.Millisecond, time.Second, "", nil, func() (string, int64, bool) { return "sha256:x", 1, true })
			if _, err := c.Decide(context.Background(), input()); err == nil {
				t.Fatal("expected fail closed")
			}
		})
	}
	c, _ := New("http://localhost", "/decision", time.Second, time.Second, "", nil, func() (string, int64, bool) { return "", 0, false })
	if _, err := c.Decide(context.Background(), input()); err == nil {
		t.Fatal("accepted stale policy")
	}
}

func TestClientRejectsAdversarialOutput(t *testing.T) {
	tests := []struct {
		name   string
		result map[string]any
	}{
		{"decision ID mismatch", map[string]any{"contract_version": policy.ContractVersion, "decision_id": "other", "allow": true, "reason_codes": []string{"agent.discover.allowed"}, "resolved_constraints": map[string]any{}, "obligations": []any{}, "decision_ttl_seconds": 1}},
		{"unknown reason", map[string]any{"contract_version": policy.ContractVersion, "decision_id": "d", "allow": true, "reason_codes": []string{"secret.detail"}, "resolved_constraints": map[string]any{}, "obligations": []any{}, "decision_ttl_seconds": 1}},
		{"mandatory obligation", map[string]any{"contract_version": policy.ContractVersion, "decision_id": "d", "allow": true, "reason_codes": []string{"agent.discover.allowed"}, "resolved_constraints": map[string]any{}, "obligations": []any{map[string]any{"type": "unknown", "mandatory": true}}, "decision_ttl_seconds": 1}},
		{"unknown field", map[string]any{"contract_version": policy.ContractVersion, "decision_id": "d", "allow": true, "reason_codes": []string{"agent.discover.allowed"}, "resolved_constraints": map[string]any{}, "obligations": []any{}, "decision_ttl_seconds": 1, "extra": "bad"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"result": tt.result})
			}))
			defer srv.Close()
			c, _ := New(srv.URL, "/decision", time.Second, time.Second, "", nil, func() (string, int64, bool) { return "sha256:x", 1, true })
			if _, err := c.Decide(context.Background(), input()); err == nil {
				t.Fatal("accepted adversarial output")
			}
		})
	}
}
