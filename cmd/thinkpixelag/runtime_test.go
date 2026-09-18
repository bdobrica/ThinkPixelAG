package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/httpserver"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

func TestRuntimeConfigurationRejectsIncompleteAuthorityAndTransport(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"policy_channel":"stable","authority_constraints":{}}`,
		`{"policy_channel":"stable","authority_constraints":{"max_llm_tokens":-1}}`,
		`{"policy_channel":"stable","authority_constraints":{"unlimited":1}}`,
		`{"policy_channel":"stable","authority_constraints":{"max_llm_tokens":1.5}}`,
		`{"policy_channel":"stable","authority_constraints":{"max_llm_tokens":1},"trusted_address":":8443"}`,
		`{"policy_channel":"stable","authority_constraints":{"max_llm_tokens":1},"unknown":true}`,
		`{"policy_channel":"stable","authority_constraints":{"max_llm_tokens":1}} {}`,
	} {
		t.Run(body, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "runtime.json")
			if err := os.WriteFile(file, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readRuntimeSettings(file); err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
	file := filepath.Join(t.TempDir(), "runtime.json")
	_ = os.WriteFile(file, []byte(`{"policy_channel":"stable","authority_constraints":{"max_llm_tokens":1000}}`), 0600)
	c, err := readRuntimeSettings(file)
	if err != nil || c.AuthorityConstraints["max_llm_tokens"].(json.Number).String() != "1000" {
		t.Fatalf("valid configuration rejected: %v", err)
	}
}

type rejectingVerifier struct{}

func (rejectingVerifier) Verify(context.Context, string) (oidc.Principal, error) {
	return oidc.Principal{}, domain.NewError(domain.CodeUnauthenticated, "invalid credential")
}
func TestRuntimePublicRoutesRejectUnauthenticatedBeforeDatabase(t *testing.T) {
	routes := runtimeRoutes{verifier: rejectingVerifier{}}
	var deps httpserver.Dependencies
	routes.mount(&deps, false)
	for name, h := range map[string]http.Handler{"discovery": deps.AgentDiscovery, "approval": deps.AgentApprovals, "admission": deps.RunAdmission, "query": deps.RunQuery, "signal": deps.RunSignal, "cancel": deps.RunCancellation, "events": deps.RunEvents, "extension": deps.ResourceExtension, "revocation": deps.Revocations} {
		for _, token := range []string{"", "Bearer forged"} {
			t.Run(name+token, func(t *testing.T) {
				req := httptest.NewRequest("GET", "/", nil)
				if token != "" {
					req.Header.Set("Authorization", token)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != http.StatusUnauthorized {
					t.Fatalf("status %d", w.Code)
				}
			})
		}
	}
	if deps.TrustedUsage != nil || deps.ResourceSettlement != nil || deps.RevocationDistribution != nil {
		t.Fatal("trusted endpoints exposed on public listener")
	}
}
func TestRuntimeSecretsRemainRedacted(t *testing.T) {
	c := config.Defaults()
	c.RuntimeFile = "runtime.json"
	c.CursorKey = config.NewSecret(strings.Repeat("cursor-secret", 4))
	b, _ := json.Marshal(c)
	if strings.Contains(string(b), "cursor-secret") {
		t.Fatal("cursor key leaked")
	}
}
