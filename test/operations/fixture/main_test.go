package main

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGatewayCountBounds(t *testing.T) {
	for _, value := range []string{"0", "-1", "5001", "invalid"} {
		if _, err := gatewayCount(value); err == nil {
			t.Fatalf("accepted invalid count %q", value)
		}
	}
	for value, want := range map[string]int{"": 5000, "1": 1, "5000": 5000} {
		got, err := gatewayCount(value)
		if err != nil || got != want {
			t.Fatalf("count %q = %d, %v", value, got, err)
		}
	}
}

func TestSampleTokenRefreshPreservesIdentityAndSigningKey(t *testing.T) {
	t.Setenv("OPS_ISSUER", "https://identity:8443")
	t.Setenv("OPS_AUDIENCE", "thinkpixelag-demo")
	t.Setenv("OPS_GATEWAY_COUNT", "1")
	t.Setenv("OPS_RETAIN_SIGNING_KEY", "1")
	dir := t.TempDir()
	if err := prepare(dir); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	keyData, err := os.ReadFile(filepath.Join(dir, "issuer.key"))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(dir, "issuer.key"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("issuer key is not private")
	}
	if err := prepare(dir); err == nil {
		t.Fatal("preparation overwrote existing identity")
	}
	var token bytes.Buffer
	if err := refreshCallerToken(dir, &token); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "identity.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("refresh changed identity or JWKS")
	}
	parts := strings.Split(strings.TrimSpace(token.String()), ".")
	if len(parts) != 3 {
		t.Fatal("invalid token shape")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err = json.Unmarshal(body, &claims); err != nil {
		t.Fatal(err)
	}
	i, err := load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != i.Issuer || claims["aud"] != i.Audience || claims["sub"] != i.Principal || claims["tenant_id"] != i.Tenant {
		t.Fatal("refresh changed caller context")
	}
	if int64(claims["exp"].(float64)-claims["iat"].(float64)) != 900 || claims["exp"].(float64) <= float64(time.Now().Unix()) {
		t.Fatal("refresh did not issue a fresh 15-minute token")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(keyData)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err = rsa.VerifyPKCS1v15(&parsed.(*rsa.PrivateKey).PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatal("token signature invalid")
	}
}

func TestTokenRefreshRequiresExplicitKeyRetention(t *testing.T) {
	t.Setenv("OPS_ISSUER", "https://identity:8443")
	t.Setenv("OPS_GATEWAY_COUNT", "1")
	t.Setenv("OPS_RETAIN_SIGNING_KEY", "")
	dir := t.TempDir()
	if err := prepare(dir); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := refreshCallerToken(dir, &out); err == nil || out.Len() != 0 {
		t.Fatal("refresh without retained key emitted a token")
	}
}
