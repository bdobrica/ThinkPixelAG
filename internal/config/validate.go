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
		"http read header timeout": c.HTTP.ReadHeaderTimeout,
		"http read timeout":        c.HTTP.ReadTimeout,
		"http write timeout":       c.HTTP.WriteTimeout,
		"http idle timeout":        c.HTTP.IdleTimeout,
		"http shutdown timeout":    c.HTTP.ShutdownTimeout,
		"database connect timeout": c.Database.ConnectTimeout,
		"opa timeout":              c.OPA.Timeout,
		"trace export timeout":     c.Telemetry.TraceExportTimeout,
		"trace batch timeout":      c.Telemetry.TraceBatchTimeout,
		"valkey timeout":           c.Valkey.Timeout,
	} {
		if value <= 0 || value > maxTimeout {
			problems = append(problems, fmt.Sprintf("%s must be greater than zero and at most %s", name, maxTimeout))
		}
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

	if len(problems) != 0 {
		return newValidationError(problems)
	}
	return nil
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
