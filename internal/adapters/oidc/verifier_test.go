package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
)

type providerState struct {
	sync.RWMutex
	key      *rsa.PrivateKey
	kid      string
	outage   bool
	requests int
}

func newProvider(t *testing.T) (*httptest.Server, *providerState) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	state := &providerState{key: key, kid: "key-1"}
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.Lock()
		defer state.Unlock()
		state.requests++
		if state.outage {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=1")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL, "jwks_uri": server.URL + "/keys"})
		case "/keys":
			n := base64.RawURLEncoding.EncodeToString(state.key.PublicKey.N.Bytes())
			e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(state.key.PublicKey.E)).Bytes())
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": state.kid, "alg": "RS256", "use": "sig", "n": n, "e": e}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, state
}

func testConfig(issuer string) config.OIDCConfig {
	return config.OIDCConfig{IssuerURL: issuer, Audience: "thinkpixelag", Algorithms: "RS256", TenantClaim: "tenant_id", RolesClaim: "groups", RoleMappings: "external-admin=policy-admin,invoke=agent-invoker", DiscoveryTimeout: time.Second, JWKSMinTTL: time.Second, JWKSMaxTTL: time.Minute, JWKSStaleTTL: time.Hour, ClockSkew: time.Second, MaxTokenAge: time.Hour}
}

func claims(issuer string, now time.Time) map[string]any {
	return map[string]any{"iss": issuer, "sub": "principal-1", "aud": "thinkpixelag", "tenant_id": "tenant-1", "groups": []string{"invoke", "unknown", "external-admin", "invoke"}, "iat": now.Unix(), "nbf": now.Add(-time.Second).Unix(), "exp": now.Add(10 * time.Minute).Unix()}
}

func signToken(t *testing.T, key *rsa.PrivateKey, kid, alg string, c map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]any{"alg": alg, "kid": kid, "typ": "JWT"})
	payload, _ := json.Marshal(c)
	encoded := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(encoded))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return encoded + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func TestVerifyMapsOnlyConfiguredIdentity(t *testing.T) {
	server, state := newProvider(t)
	verifier, err := New(context.Background(), testConfig(server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	verifier.now = func() time.Time { return now }
	state.RLock()
	token := signToken(t, state.key, state.kid, "RS256", claims(server.URL, now))
	state.RUnlock()
	p, err := verifier.Verify(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "principal-1" || p.TenantID != "tenant-1" || fmt.Sprint(p.Roles) != "[agent-invoker policy-admin]" {
		t.Fatalf("principal = %#v", p)
	}
}

func TestVerifyRejectsAuthenticationFailures(t *testing.T) {
	server, state := newProvider(t)
	verifier, err := New(context.Background(), testConfig(server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	verifier.now = func() time.Time { return now }
	state.RLock()
	trustedKey, kid := state.key, state.kid
	state.RUnlock()
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	tests := []struct {
		name   string
		alg    string
		key    *rsa.PrivateKey
		mutate func(map[string]any)
	}{
		{name: "bad signature", alg: "RS256", key: other},
		{name: "issuer", alg: "RS256", key: trustedKey, mutate: func(c map[string]any) { c["iss"] = "https://evil.example" }},
		{name: "audience", alg: "RS256", key: trustedKey, mutate: func(c map[string]any) { c["aud"] = "other" }},
		{name: "algorithm", alg: "RS512", key: trustedKey},
		{name: "expiry", alg: "RS256", key: trustedKey, mutate: func(c map[string]any) { c["exp"] = now.Add(-2 * time.Second).Unix() }},
		{name: "not before", alg: "RS256", key: trustedKey, mutate: func(c map[string]any) { c["nbf"] = now.Add(2 * time.Second).Unix() }},
		{name: "missing tenant", alg: "RS256", key: trustedKey, mutate: func(c map[string]any) { delete(c, "tenant_id") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := claims(server.URL, now)
			if tt.mutate != nil {
				tt.mutate(c)
			}
			token := signToken(t, tt.key, kid, tt.alg, c)
			_, err := verifier.Verify(context.Background(), token)
			if domain.ErrorCodeOf(err) != domain.CodeUnauthenticated {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestKeyRotationAndBoundedOutage(t *testing.T) {
	server, state := newProvider(t)
	verifier, err := New(context.Background(), testConfig(server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	verifier.now = func() time.Time { return now }
	rotated, _ := rsa.GenerateKey(rand.Reader, 2048)
	state.Lock()
	state.key, state.kid = rotated, "key-2"
	state.Unlock()
	token := signToken(t, rotated, "key-2", "RS256", claims(server.URL, now))
	if _, err := verifier.Verify(context.Background(), token); err != nil {
		t.Fatalf("rotation: %v", err)
	}
	state.Lock()
	state.outage = true
	state.Unlock()
	now = now.Add(2 * time.Minute)
	if _, err := verifier.Verify(context.Background(), token); err != nil {
		t.Fatalf("cached key during bounded outage: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := verifier.Verify(context.Background(), token); domain.ErrorCodeOf(err) != domain.CodeUnavailable {
		t.Fatalf("stale outage error = %v", err)
	}
}

func TestDiscoveryPinsIssuerAndJWKSOrigin(t *testing.T) {
	foreign := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer foreign.Close()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL, "jwks_uri": foreign.URL + "/keys"})
	}))
	defer server.Close()
	if _, err := New(context.Background(), testConfig(server.URL), server.Client()); err == nil {
		t.Fatal("New accepted cross-origin JWKS")
	}
}
