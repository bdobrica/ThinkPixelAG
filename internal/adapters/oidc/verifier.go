// Package oidc verifies access tokens against one explicitly configured OIDC issuer.
package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/identity"
)

const maxDocumentBytes = 1 << 20

type Principal = identity.Principal

type Verifier interface {
	Verify(context.Context, string) (Principal, error)
}

type keyEntry struct {
	key crypto.PublicKey
	alg string
}

type TokenVerifier struct {
	config             config.OIDCConfig
	client             *http.Client
	now                func() time.Time
	algorithms         map[string]bool
	roleMappings       map[string]string
	mu                 sync.RWMutex
	jwksURL            string
	keys               map[string]keyEntry
	expiresAt, staleAt time.Time
}

func New(ctx context.Context, cfg config.OIDCConfig, client *http.Client) (*TokenVerifier, error) {
	if client == nil {
		client = http.DefaultClient
	}
	v := &TokenVerifier{config: cfg, client: client, now: time.Now, algorithms: csvSet(cfg.Algorithms), roleMappings: mappings(cfg.RoleMappings)}
	if err := v.discoverAndRefresh(ctx); err != nil {
		return nil, fmt.Errorf("initialize OIDC verifier: %w", err)
	}
	return v, nil
}

func (v *TokenVerifier) Verify(ctx context.Context, token string) (Principal, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(token) > 64<<10 {
		return Principal{}, unauth("access token is invalid", nil)
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
	}
	if err := decodePart(parts[0], &header); err != nil || !v.algorithms[header.Algorithm] || header.KeyID == "" {
		return Principal{}, unauth("access token is invalid", err)
	}
	entry, err := v.key(ctx, header.KeyID, header.Algorithm)
	if err != nil {
		return Principal{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !verifySignature(entry.key, header.Algorithm, digest[:], signature) {
		return Principal{}, unauth("access token signature is invalid", err)
	}
	var claims map[string]any
	if err := decodePart(parts[1], &claims); err != nil {
		return Principal{}, unauth("access token claims are invalid", err)
	}
	return v.mapClaims(claims)
}

func (v *TokenVerifier) key(ctx context.Context, kid, alg string) (keyEntry, error) {
	v.mu.RLock()
	entry, ok := v.keys[kid]
	expires, stale := v.expiresAt, v.staleAt
	v.mu.RUnlock()
	now := v.now()
	if ok && entry.alg == alg && now.Before(expires) {
		return entry, nil
	}
	refreshErr := v.refresh(ctx)
	v.mu.RLock()
	entry, ok = v.keys[kid]
	stale = v.staleAt
	v.mu.RUnlock()
	if ok && entry.alg == alg && now.Before(stale) {
		return entry, nil
	}
	if refreshErr != nil {
		return keyEntry{}, domain.WrapError(domain.CodeUnavailable, "identity provider keys are unavailable", refreshErr).WithRetryable()
	}
	return keyEntry{}, unauth("access token signing key is unknown", nil)
}

func (v *TokenVerifier) mapClaims(c map[string]any) (Principal, error) {
	now := v.now().UTC()
	issuer, _ := c["iss"].(string)
	subject, _ := c["sub"].(string)
	tenant, _ := c[v.config.TenantClaim].(string)
	if issuer != strings.TrimSuffix(v.config.IssuerURL, "/") || subject == "" || tenant == "" {
		return Principal{}, unauth("access token identity claims are invalid", nil)
	}
	if !hasAudience(c["aud"], v.config.Audience) {
		return Principal{}, unauth("access token audience is invalid", nil)
	}
	exp, okExp := number(c["exp"])
	iat, okIAT := number(c["iat"])
	nbf, okNBF := number(c["nbf"])
	if !okExp || !okIAT || now.After(time.Unix(exp, 0).Add(v.config.ClockSkew)) || time.Unix(iat, 0).After(now.Add(v.config.ClockSkew)) || time.Unix(exp, 0).Sub(time.Unix(iat, 0)) > v.config.MaxTokenAge || (okNBF && time.Unix(nbf, 0).After(now.Add(v.config.ClockSkew))) {
		return Principal{}, unauth("access token time claims are invalid", nil)
	}
	roles := make([]string, 0)
	if values, ok := stringSlice(c[v.config.RolesClaim]); ok {
		seen := map[string]bool{}
		for _, external := range values {
			if mapped, exists := v.roleMappings[external]; exists && !seen[mapped] {
				roles = append(roles, mapped)
				seen[mapped] = true
			}
		}
	}
	sort.Strings(roles)
	return Principal{ID: subject, TenantID: tenant, Issuer: issuer, Roles: roles}, nil
}

func (v *TokenVerifier) discoverAndRefresh(ctx context.Context) error {
	issuer := strings.TrimSuffix(v.config.IssuerURL, "/")
	var discovery struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if _, err := v.getJSON(ctx, issuer+"/.well-known/openid-configuration", &discovery); err != nil {
		return err
	}
	if discovery.Issuer != issuer || !sameSecureOrigin(issuer, discovery.JWKSURI) {
		return errors.New("OIDC discovery issuer or JWKS URI violates configured trust boundary")
	}
	v.mu.Lock()
	v.jwksURL = discovery.JWKSURI
	v.mu.Unlock()
	return v.refresh(ctx)
}

func (v *TokenVerifier) refresh(ctx context.Context) error {
	v.mu.RLock()
	endpoint := v.jwksURL
	v.mu.RUnlock()
	var document struct {
		Keys []json.RawMessage `json:"keys"`
	}
	ttl, err := v.getJSON(ctx, endpoint, &document)
	if err != nil {
		return err
	}
	keys := map[string]keyEntry{}
	for _, raw := range document.Keys {
		kid, entry, parseErr := parseJWK(raw)
		if parseErr == nil && v.algorithms[entry.alg] {
			if _, duplicate := keys[kid]; duplicate {
				return errors.New("duplicate JWKS key id")
			}
			keys[kid] = entry
		}
	}
	if len(keys) == 0 {
		return errors.New("JWKS has no usable configured signing keys")
	}
	if ttl < v.config.JWKSMinTTL {
		ttl = v.config.JWKSMinTTL
	}
	if ttl > v.config.JWKSMaxTTL {
		ttl = v.config.JWKSMaxTTL
	}
	now := v.now()
	v.mu.Lock()
	v.keys, v.expiresAt, v.staleAt = keys, now.Add(ttl), now.Add(v.config.JWKSStaleTTL)
	v.mu.Unlock()
	return nil
}

func (v *TokenVerifier) getJSON(parent context.Context, endpoint string, destination any) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(parent, v.config.DiscoveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.Request == nil || !sameSecureOrigin(endpoint, resp.Request.URL.String()) {
		return 0, errors.New("identity provider redirect crossed the configured origin")
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("identity provider returned HTTP %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxDocumentBytes+1))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return 0, err
	}
	return cacheTTL(resp.Header.Get("Cache-Control")), nil
}

func parseJWK(raw []byte) (string, keyEntry, error) {
	var j struct{ Kty, Kid, Alg, Use, N, E, Crv, X, Y string }
	if err := json.Unmarshal(raw, &j); err != nil || j.Kid == "" || j.Alg == "" || (j.Use != "" && j.Use != "sig") {
		return "", keyEntry{}, errors.New("invalid JWK metadata")
	}
	switch j.Kty {
	case "RSA":
		n, err := base64.RawURLEncoding.DecodeString(j.N)
		if err != nil {
			return "", keyEntry{}, err
		}
		eb, err := base64.RawURLEncoding.DecodeString(j.E)
		if err != nil || len(eb) == 0 || len(eb) > 4 {
			return "", keyEntry{}, errors.New("invalid RSA exponent")
		}
		e := 0
		for _, b := range eb {
			e = e<<8 | int(b)
		}
		if e < 3 {
			return "", keyEntry{}, errors.New("invalid RSA exponent")
		}
		return j.Kid, keyEntry{key: &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: e}, alg: j.Alg}, nil
	case "EC":
		if j.Crv != "P-256" {
			return "", keyEntry{}, errors.New("unsupported EC curve")
		}
		xb, xerr := base64.RawURLEncoding.DecodeString(j.X)
		yb, yerr := base64.RawURLEncoding.DecodeString(j.Y)
		if xerr != nil || yerr != nil {
			return "", keyEntry{}, errors.New("invalid EC key")
		}
		key := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(xb), Y: new(big.Int).SetBytes(yb)}
		if !key.Curve.IsOnCurve(key.X, key.Y) {
			return "", keyEntry{}, errors.New("invalid EC point")
		}
		return j.Kid, keyEntry{key: key, alg: j.Alg}, nil
	default:
		return "", keyEntry{}, errors.New("unsupported JWK type")
	}
}

func verifySignature(key crypto.PublicKey, alg string, digest, signature []byte) bool {
	switch typed := key.(type) {
	case *rsa.PublicKey:
		return alg == "RS256" && rsa.VerifyPKCS1v15(typed, crypto.SHA256, digest, signature) == nil
	case *ecdsa.PublicKey:
		if alg != "ES256" || len(signature) != 64 {
			return false
		}
		return ecdsa.Verify(typed, digest, new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:]))
	default:
		return false
	}
}

func decodePart(value string, out any) error {
	b, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JWT JSON values")
	}
	return nil
}
func unauth(detail string, cause error) error {
	return domain.WrapError(domain.CodeUnauthenticated, detail, cause)
}
func csvSet(v string) map[string]bool {
	out := map[string]bool{}
	for _, s := range strings.Split(v, ",") {
		out[s] = true
	}
	return out
}
func mappings(v string) map[string]string {
	out := map[string]string{}
	if v == "" {
		return out
	}
	for _, p := range strings.Split(v, ",") {
		a, b, _ := strings.Cut(p, "=")
		out[a] = b
	}
	return out
}
func number(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	x, err := strconv.ParseInt(string(n), 10, 64)
	return x, err == nil
}
func hasAudience(v any, want string) bool {
	if s, ok := v.(string); ok {
		return s == want
	}
	values, ok := stringSlice(v)
	if !ok {
		return false
	}
	for _, s := range values {
		if s == want {
			return true
		}
	}
	return false
}
func stringSlice(v any) ([]string, bool) {
	raw, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(raw))
	for i := range raw {
		out[i], ok = raw[i].(string)
		if !ok {
			return nil, false
		}
	}
	return out, true
}
func cacheTTL(value string) time.Duration {
	for _, p := range strings.Split(value, ",") {
		p = strings.TrimSpace(p)
		if s, ok := strings.CutPrefix(p, "max-age="); ok {
			n, err := strconv.ParseInt(strings.Trim(s, `"`), 10, 64)
			if err == nil && n >= 0 {
				return time.Duration(n) * time.Second
			}
		}
	}
	return 0
}
func sameSecureOrigin(issuer, target string) bool {
	a, e1 := url.Parse(issuer)
	b, e2 := url.Parse(target)
	if e1 != nil || e2 != nil {
		return false
	}
	secure := a.Scheme == "https" && b.Scheme == "https"
	loopback := a.Scheme == "http" && b.Scheme == "http" && isLoopbackHost(a.Hostname()) && isLoopbackHost(b.Hostname())
	return (secure || loopback) && strings.EqualFold(a.Host, b.Host) && b.User == nil && b.RawQuery == "" && b.Fragment == ""
}

func isLoopbackHost(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
