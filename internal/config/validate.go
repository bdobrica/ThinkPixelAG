package config

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const maxTimeout = 5 * time.Minute

// ValidationError reports all independently detectable startup problems.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string {
	return "invalid configuration: " + strings.Join(e.Problems, "; ")
}

func newValidationError(problems []string) error {
	sort.Strings(problems)
	return &ValidationError{Problems: problems}
}

// Validate checks required values, types, bounds, URL schemes, and production
// transport requirements without performing network I/O.
func (c Config) Validate() error {
	var problems []string
	if c.Environment != EnvironmentLocal && c.Environment != EnvironmentTest && c.Environment != EnvironmentProduction {
		problems = append(problems, "environment must be local, test, or production")
	}
	if err := validateAddress(c.HTTP.Address); err != nil {
		problems = append(problems, "http address: "+err.Error())
	}
	for name, value := range map[string]time.Duration{
		"http read header timeout":   c.HTTP.ReadHeaderTimeout,
		"http read timeout":          c.HTTP.ReadTimeout,
		"http handler timeout":       c.HTTP.HandlerTimeout,
		"http write timeout":         c.HTTP.WriteTimeout,
		"http idle timeout":          c.HTTP.IdleTimeout,
		"http shutdown timeout":      c.HTTP.ShutdownTimeout,
		"database connect timeout":   c.Database.ConnectTimeout,
		"database health timeout":    c.Database.HealthTimeout,
		"database statement timeout": c.Database.StatementTimeout,
		"database lock timeout":      c.Database.LockTimeout,
		"opa timeout":                c.OPA.Timeout,
		"opa decision max TTL":       c.OPA.DecisionMaxTTL,
		"opa bundle max age":         c.OPA.BundleMaxAge,
		"trace export timeout":       c.Telemetry.TraceExportTimeout,
		"trace batch timeout":        c.Telemetry.TraceBatchTimeout,
		"valkey timeout":             c.Valkey.Timeout,
		"oidc discovery timeout":     c.OIDC.DiscoveryTimeout,
		"evidence timeout":           c.Evidence.Timeout,
	} {
		if value <= 0 || value > maxTimeout {
			problems = append(problems, fmt.Sprintf("%s must be greater than zero and at most %s", name, maxTimeout))
		}
	}
	for name, value := range map[string]time.Duration{"database max connection lifetime": c.Database.MaxConnectionLifetime, "database max connection idle time": c.Database.MaxConnectionIdleTime} {
		if value <= 0 || value > 24*time.Hour {
			problems = append(problems, fmt.Sprintf("%s must be greater than zero and at most 24h0m0s", name))
		}
	}
	if c.Database.MinConnections < 0 || c.Database.MaxConnections < 1 || c.Database.MaxConnections > 1000 || c.Database.MinConnections > c.Database.MaxConnections {
		problems = append(problems, "database connections must satisfy 0 <= min <= max <= 1000")
	}
	if c.HTTP.MaxHeaderBytes < 1024 || c.HTTP.MaxHeaderBytes > 16<<20 {
		problems = append(problems, "http max header bytes must be from 1024 through 16777216")
	}
	if c.HTTP.MaxBodyBytes < 1 || c.HTTP.MaxBodyBytes > 64<<20 {
		problems = append(problems, "http max body bytes must be from 1 through 67108864")
	}
	if !c.Database.URL.IsSet() {
		problems = append(problems, "database URL is required")
	} else if err := validateSecretURL(c.Database.URL, "postgres", "postgresql"); err != nil {
		problems = append(problems, "database URL: "+err.Error())
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		problems = append(problems, "log level must be debug, info, warn, or error")
	}
	if err := validateHTTPURL(c.OPA.URL, c.Environment == EnvironmentProduction); err != nil {
		problems = append(problems, "OPA URL: "+err.Error())
	}
	if !strings.HasPrefix(c.OPA.DecisionPath, "/") || strings.ContainsAny(c.OPA.DecisionPath, "?#") {
		problems = append(problems, "OPA decision path must be an absolute path without query or fragment")
	}
	if c.Telemetry.TracingMode != "noop" && c.Telemetry.TracingMode != "otlp" {
		problems = append(problems, "tracing mode must be noop or otlp")
	}
	if strings.TrimSpace(c.Telemetry.ServiceName) == "" || strings.TrimSpace(c.Telemetry.ServiceName) != c.Telemetry.ServiceName || len(c.Telemetry.ServiceName) > 64 || strings.IndexFunc(c.Telemetry.ServiceName, unicode.IsControl) >= 0 {
		problems = append(problems, "service name must be 1 through 64 bytes without surrounding whitespace or control characters")
	}
	if math.IsNaN(c.Telemetry.TraceSampleRatio) || math.IsInf(c.Telemetry.TraceSampleRatio, 0) || c.Telemetry.TraceSampleRatio < 0 || c.Telemetry.TraceSampleRatio > 1 {
		problems = append(problems, "trace sample ratio must be a finite number from 0 through 1")
	}
	if c.Telemetry.TracingMode == "otlp" {
		if err := validateHTTPURL(c.Telemetry.OTLPEndpoint, c.Environment == EnvironmentProduction); err != nil {
			problems = append(problems, "OTLP endpoint: "+err.Error())
		}
	}
	if c.Valkey.URL.IsSet() {
		if err := validateSecretURL(c.Valkey.URL, "redis", "rediss"); err != nil {
			problems = append(problems, "Valkey URL: "+err.Error())
		}
		if c.Environment == EnvironmentProduction && urlScheme(c.Valkey.URL.Value()) != "rediss" && !isLoopbackURL(c.Valkey.URL.Value()) {
			problems = append(problems, "Valkey URL must use rediss in production unless it targets loopback")
		}
		if len(c.Valkey.CacheIntegrityKey.Value()) < 32 {
			problems = append(problems, "Valkey cache HMAC key must be at least 32 bytes when Valkey is enabled")
		}
	} else if c.Valkey.CacheIntegrityKey.IsSet() {
		problems = append(problems, "Valkey cache HMAC key requires a Valkey URL")
	}
	if strings.TrimSpace(c.OIDC.Audience) == "" {
		problems = append(problems, "OIDC audience is required")
	}
	if strings.TrimSpace(c.OIDC.Audience) != c.OIDC.Audience || strings.IndexFunc(c.OIDC.Audience, unicode.IsControl) >= 0 {
		problems = append(problems, "OIDC audience must not contain surrounding whitespace or control characters")
	}
	if err := validateHTTPURL(c.OIDC.IssuerURL, true); err != nil {
		problems = append(problems, "OIDC issuer URL: "+err.Error())
	}
	if c.OIDC.JWKSMinTTL <= 0 || c.OIDC.JWKSMaxTTL < c.OIDC.JWKSMinTTL || c.OIDC.JWKSStaleTTL < c.OIDC.JWKSMaxTTL {
		problems = append(problems, "OIDC JWKS TTLs must satisfy 0 < min <= max <= stale")
	}
	if c.OIDC.ClockSkew < 0 || c.OIDC.ClockSkew > 5*time.Minute || c.OIDC.MaxTokenAge <= 0 || c.OIDC.MaxTokenAge > 7*24*time.Hour {
		problems = append(problems, "OIDC time bounds require clock skew from 0 through 5m and max token age from 1ns through 168h")
	}
	if !validOIDCAlgorithms(c.OIDC.Algorithms) {
		problems = append(problems, "OIDC algorithms must be a unique comma-separated subset of RS256,ES256")
	}
	if !validClaimName(c.OIDC.TenantClaim) || !validClaimName(c.OIDC.RolesClaim) || c.OIDC.TenantClaim == c.OIDC.RolesClaim {
		problems = append(problems, "OIDC tenant and roles claims must be distinct safe claim names")
	}
	if !validRoleMappings(c.OIDC.RoleMappings) {
		problems = append(problems, "OIDC role mappings must be unique comma-separated external=internal pairs")
	}
	if err := validateSigning(c.Signing, c.Environment); err != nil {
		problems = append(problems, "signing: "+err.Error())
	}
	if err := validateEvidenceSink(c.Evidence, c.Environment); err != nil {
		problems = append(problems, "evidence: "+err.Error())
	}

	if len(problems) != 0 {
		return newValidationError(problems)
	}
	return nil
}

func validateEvidenceSink(evidence EvidenceConfig, _ Environment) error {
	configured := evidence.SinkID != "" || evidence.Endpoint != "" || evidence.BearerToken.IsSet()
	if !configured {
		return nil
	}
	if strings.TrimSpace(evidence.SinkID) != evidence.SinkID || evidence.SinkID == "" || len(evidence.SinkID) > 256 || strings.IndexFunc(evidence.SinkID, unicode.IsControl) >= 0 {
		return fmt.Errorf("sink ID must be 1 through 256 canonical bytes")
	}
	if err := validateHTTPURL(evidence.Endpoint, true); err != nil {
		return fmt.Errorf("endpoint: %w", err)
	}
	if urlScheme(evidence.Endpoint) != "https" {
		return fmt.Errorf("endpoint must use https")
	}
	if !evidence.BearerToken.IsSet() || strings.ContainsAny(evidence.BearerToken.Value(), "\r\n") {
		return fmt.Errorf("bearer token is required and must not contain line breaks")
	}
	if evidence.MaxResponseBytes < 1 || evidence.MaxResponseBytes > 1<<20 {
		return fmt.Errorf("maximum response bytes must be from 1 through 1048576")
	}
	return nil
}

func validateSigning(signing SigningConfig, environment Environment) error {
	if signing.Provider != "disabled" && signing.Provider != "kms" && signing.Provider != "hsm" {
		return fmt.Errorf("provider must be disabled, kms, or hsm")
	}
	if signing.Algorithm != "ED25519" && signing.Algorithm != "ECDSA_SHA256" && signing.Algorithm != "RSA_PSS_SHA256" {
		return fmt.Errorf("algorithm is not supported")
	}
	if signing.Provider == "disabled" {
		if signing.KeyID != "" {
			return fmt.Errorf("key ID requires a managed provider")
		}
		if environment == EnvironmentProduction {
			return fmt.Errorf("production requires a kms or hsm provider")
		}
		return nil
	}
	if signing.KeyID == "" || strings.TrimSpace(signing.KeyID) != signing.KeyID || len(signing.KeyID) > 512 || strings.IndexFunc(signing.KeyID, unicode.IsControl) >= 0 {
		return fmt.Errorf("managed key ID must be 1 through 512 canonical bytes")
	}
	lowerKeyID := strings.ToLower(signing.KeyID)
	if strings.HasPrefix(lowerKeyID, "file:") || strings.HasPrefix(lowerKeyID, "/") ||
		strings.HasPrefix(lowerKeyID, "./") || strings.HasPrefix(lowerKeyID, "../") ||
		strings.Contains(lowerKeyID, "-----begin") {
		return fmt.Errorf("private keys and file references are not configurable")
	}
	return nil
}

func validOIDCAlgorithms(value string) bool {
	seen := map[string]bool{}
	for _, item := range strings.Split(value, ",") {
		if (item != "RS256" && item != "ES256") || seen[item] {
			return false
		}
		seen[item] = true
	}
	return len(seen) > 0
}

func validClaimName(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r == '_' || r == '-' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

func validRoleMappings(value string) bool {
	if value == "" {
		return true
	}
	seen := map[string]bool{}
	for _, pair := range strings.Split(value, ",") {
		left, right, ok := strings.Cut(pair, "=")
		if !ok || !validClaimName(left) || !validClaimName(right) || seen[left] {
			return false
		}
		seen[left] = true
	}
	return true
}

func validateAddress(address string) error {
	if strings.TrimSpace(address) != address || address == "" {
		return fmt.Errorf("must be a non-empty host:port without surrounding whitespace")
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return fmt.Errorf("must be a valid host:port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("port must be a number from 1 through 65535")
	}
	return nil
}

func validateSecretURL(value Secret, schemes ...string) error {
	parsed, err := url.Parse(value.Value())
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("must be an absolute URL")
	}
	for _, scheme := range schemes {
		if parsed.Scheme == scheme {
			return nil
		}
	}
	return fmt.Errorf("scheme must be one of %s", strings.Join(schemes, ", "))
}

func validateHTTPURL(value string, requireHTTPS bool) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("must be an absolute HTTP(S) URL without user information")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("must not contain a query or fragment")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if requireHTTPS && parsed.Scheme != "https" && !isLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("must use https unless it targets loopback")
	}
	return nil
}

func urlScheme(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return parsed.Scheme
}

func isLoopbackURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && isLoopbackHost(parsed.Hostname())
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
