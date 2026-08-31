package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func validEnvironment() map[string]string {
	return map[string]string{
		"THINKPIXELAG_DATABASE_URL":    "postgresql://app:database-secret@db.example/thinkpixelag?sslmode=verify-full",
		"THINKPIXELAG_OIDC_ISSUER_URL": "https://id.example/issuer",
		"THINKPIXELAG_OIDC_AUDIENCE":   "thinkpixelag",
	}
}

func TestLoadDefaultsEnvironmentAndFlagPrecedence(t *testing.T) {
	t.Parallel()
	environment := validEnvironment()
	environment["THINKPIXELAG_HTTP_ADDRESS"] = "127.0.0.1:8081"
	environment["THINKPIXELAG_HTTP_MAX_BODY_BYTES"] = "2048"
	environment["THINKPIXELAG_OPA_TIMEOUT"] = "3s"
	environment["THINKPIXELAG_OPA_DECISION_MAX_TTL"] = "20s"

	environment["THINKPIXELAG_LOG_LEVEL"] = "warn"
	environment["THINKPIXELAG_METRICS_ENABLED"] = "false"
	environment["THINKPIXELAG_TRACE_SAMPLE_RATIO"] = "0.25"
	environment["THINKPIXELAG_DATABASE_MIN_CONNECTIONS"] = "2"
	c, err := load([]string{"--http-address=127.0.0.1:9090", "--http-max-body-bytes=4096", "--http-handler-timeout=10s", "--opa-timeout=4s", "--opa-bundle-max-age=4m", "--log-level=error", "--metrics-enabled=true", "--trace-sample-ratio=0.5", "--database-max-connections=24"}, environment)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if c.HTTP.Address != "127.0.0.1:9090" {
		t.Errorf("HTTP address = %q, want flag value", c.HTTP.Address)
	}
	if c.OPA.Timeout != 4*time.Second {
		t.Errorf("OPA timeout = %s, want 4s", c.OPA.Timeout)
	}
	if c.OPA.DecisionMaxTTL != 20*time.Second || c.OPA.BundleMaxAge != 4*time.Minute {
		t.Errorf("OPA policy bounds = %#v", c.OPA)
	}
	if c.HTTP.ReadTimeout != 15*time.Second {
		t.Errorf("HTTP read timeout = %s, want default 15s", c.HTTP.ReadTimeout)
	}
	if c.HTTP.MaxBodyBytes != 4096 || c.HTTP.HandlerTimeout != 10*time.Second {
		t.Errorf("HTTP bounds = %d, %s", c.HTTP.MaxBodyBytes, c.HTTP.HandlerTimeout)
	}
	if c.Log.Level != "error" {
		t.Errorf("log level = %q, want flag value", c.Log.Level)
	}
	if !c.Telemetry.MetricsEnabled || c.Telemetry.TraceSampleRatio != 0.5 {
		t.Errorf("telemetry flag precedence failed: %#v", c.Telemetry)
	}
	if got := c.Database.URL.Value(); !strings.Contains(got, "database-secret") {
		t.Errorf("database secret was not loaded")
	}
	if c.Database.MinConnections != 2 || c.Database.MaxConnections != 24 || c.Database.StatementTimeout != 10*time.Second {
		t.Errorf("database pool policy was not loaded: %#v", c.Database)
	}
}

func TestLoadOIDCTrustBounds(t *testing.T) {
	t.Parallel()
	environment := validEnvironment()
	environment["THINKPIXELAG_OIDC_ALGORITHMS"] = "RS256,ES256"
	environment["THINKPIXELAG_OIDC_ROLE_MAPPINGS"] = "policy-operators=policy-admin,users=agent-invoker"
	environment["THINKPIXELAG_OIDC_JWKS_MAX_TTL"] = "30m"
	c, err := load([]string{"--oidc-clock-skew=15s", "--oidc-tenant-claim=organization_id"}, environment)
	if err != nil {
		t.Fatal(err)
	}
	if c.OIDC.Algorithms != "RS256,ES256" || c.OIDC.JWKSMaxTTL != 30*time.Minute || c.OIDC.ClockSkew != 15*time.Second || c.OIDC.TenantClaim != "organization_id" {
		t.Fatalf("OIDC config = %#v", c.OIDC)
	}
}

func TestValidateRejectsUnsafeOIDCTrustConfiguration(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Database.URL = NewSecret("postgres://db.example/service")
	c.OIDC.IssuerURL = "https://id.example"
	c.OIDC.Audience = "api"
	c.OIDC.Algorithms = "RS256,none"
	c.OIDC.TenantClaim = "tenant id"
	c.OIDC.RoleMappings = "admin"
	c.OIDC.JWKSStaleTTL = time.Second
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate accepted unsafe OIDC configuration")
	}
	for _, want := range []string{"OIDC algorithms", "OIDC tenant and roles claims", "OIDC role mappings", "OIDC JWKS TTLs"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
}

func TestLoadRejectsUnknownAndMalformedInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr string
	}{
		{name: "unknown environment", env: map[string]string{"THINKPIXELAG_DATABASE_URl": "typo"}, wantErr: "unknown environment variable"},
		{name: "malformed duration", env: map[string]string{"THINKPIXELAG_OPA_TIMEOUT": "soon"}, wantErr: "must be a Go duration"},
		{name: "malformed bool", env: map[string]string{"THINKPIXELAG_METRICS_ENABLED": "sometimes"}, wantErr: "must be true or false"},
		{name: "malformed ratio", env: map[string]string{"THINKPIXELAG_TRACE_SAMPLE_RATIO": "many"}, wantErr: "must be a number"},
		{name: "malformed bytes", env: map[string]string{"THINKPIXELAG_HTTP_MAX_BODY_BYTES": "many"}, wantErr: "must be an integer"},
		{name: "malformed connections", env: map[string]string{"THINKPIXELAG_DATABASE_MAX_CONNECTIONS": "many"}, wantErr: "32-bit integer"},
		{name: "unknown flag", args: []string{"--database-url=secret"}, env: validEnvironment(), wantErr: "flag provided but not defined"},
		{name: "positional argument", args: []string{"serve"}, env: validEnvironment(), wantErr: "unexpected positional"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := load(tt.args, tt.env)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("load() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRejectsDatabasePoolBounds(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Database.URL = NewSecret("postgres://db.example/service")
	c.Database.MinConnections = 5
	c.Database.MaxConnections = 4
	c.Database.StatementTimeout = 0
	c.OIDC.IssuerURL = "https://id.example/issuer"
	c.OIDC.Audience = "thinkpixelag"
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "database connections") || !strings.Contains(err.Error(), "database statement timeout") {
		t.Fatalf("Validate() error = %v, want database pool failures", err)
	}
}

func TestValidateAggregatesProblems(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Environment = "staging"
	c.HTTP.Address = "not-an-address"
	c.OPA.Timeout = 0
	c.OPA.DecisionPath = "relative?query=yes"
	c.OIDC.Audience = " audience\t"

	err := c.Validate()
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("Validate() error = %T %v, want *ValidationError", err, err)
	}
	if len(validationErr.Problems) < 6 {
		t.Fatalf("Validate() returned %d problems, want aggregated failures: %v", len(validationErr.Problems), validationErr.Problems)
	}
	for _, want := range []string{"database URL is required", "environment must", "http address", "OIDC audience", "OIDC issuer", "opa timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error missing %q: %v", want, err)
		}
	}
}

func TestValidateRejectsNamedPort(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.HTTP.Address = ":http"
	c.Database.URL = NewSecret("postgres://db.example/service")
	c.OIDC.IssuerURL = "https://id.example/issuer"
	c.OIDC.Audience = "thinkpixelag"

	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "port must be a number") {
		t.Fatalf("Validate() error = %v, want numeric port failure", err)
	}
}

func TestValidateRejectsLogLevel(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Log.Level = "verbose"
	c.Database.URL = NewSecret("postgres://db.example/service")
	c.OIDC.IssuerURL = "https://id.example/issuer"
	c.OIDC.Audience = "thinkpixelag"

	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "log level") {
		t.Fatalf("Validate() error = %v, want log level failure", err)
	}
}

func TestValidateRejectsTelemetrySettings(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Database.URL = NewSecret("postgres://db.example/service")
	c.OIDC.IssuerURL = "https://id.example/issuer"
	c.OIDC.Audience = "thinkpixelag"
	c.Telemetry.TracingMode = "console"
	c.Telemetry.ServiceName = " service\t"
	c.Telemetry.TraceSampleRatio = 2
	c.Telemetry.TraceExportTimeout = 0

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want telemetry failures")
	}
	for _, want := range []string{"tracing mode", "service name", "trace sample ratio", "trace export timeout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error missing %q: %v", want, err)
		}
	}
}

func TestValidateRejectsInsecureProductionOTLP(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Environment = EnvironmentProduction
	c.Database.URL = NewSecret("postgres://db.example/service")
	c.OIDC.IssuerURL = "https://id.example/issuer"
	c.OIDC.Audience = "thinkpixelag"
	c.Telemetry.TracingMode = "otlp"
	c.Telemetry.OTLPEndpoint = "http://collector.example:4318"

	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "OTLP endpoint") {
		t.Fatalf("Validate() error = %v, want insecure OTLP failure", err)
	}
}

func TestValidateProductionTransport(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Environment = EnvironmentProduction
	c.Database.URL = NewSecret("postgres://app:secret@db.example/thinkpixelag")
	c.OPA.URL = "http://opa.example"
	c.Valkey.URL = NewSecret("redis://cache.example:6379")
	c.Valkey.CacheIntegrityKey = NewSecret("0123456789abcdef0123456789abcdef")
	c.OIDC.IssuerURL = "http://id.example/issuer"
	c.OIDC.Audience = "thinkpixelag"

	err := c.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want insecure transport failures")
	}
	for _, want := range []string{"OPA URL", "Valkey URL", "OIDC issuer URL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error missing %q: %v", want, err)
		}
	}
}

func TestSigningConfigurationRequiresManagedProductionKey(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Database.URL = NewSecret("postgres://db.example/service")
	c.OIDC.IssuerURL = "https://id.example/issuer"
	c.OIDC.Audience = "thinkpixelag"
	c.Environment = EnvironmentProduction
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "production requires a kms or hsm provider") {
		t.Fatalf("Validate() error = %v, want managed signing provider failure", err)
	}
	c.Signing = SigningConfig{Provider: "kms", KeyID: "arn:example:kms:key/policy", Algorithm: "ECDSA_SHA256"}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid managed signing configuration: %v", err)
	}
}

func TestLoadManagedSigningReference(t *testing.T) {
	t.Parallel()
	environment := validEnvironment()
	environment["THINKPIXELAG_ENVIRONMENT"] = "production"
	environment["THINKPIXELAG_SIGNING_PROVIDER"] = "hsm"
	environment["THINKPIXELAG_SIGNING_KEY_ID"] = "pkcs11:object=policy-signing-key"
	environment["THINKPIXELAG_SIGNING_ALGORITHM"] = "RSA_PSS_SHA256"
	c, err := load(nil, environment)
	if err != nil {
		t.Fatal(err)
	}
	if c.Signing.Provider != "hsm" || c.Signing.KeyID != "pkcs11:object=policy-signing-key" || c.Signing.Algorithm != "RSA_PSS_SHA256" {
		t.Fatalf("signing config = %#v", c.Signing)
	}
}

func TestSigningConfigurationRejectsPrivateKeyAndFileInputs(t *testing.T) {
	t.Parallel()
	base := validEnvironment()
	base["THINKPIXELAG_SIGNING_PROVIDER"] = "kms"
	base["THINKPIXELAG_SIGNING_KEY_ID"] = "/secrets/private.pem"
	if _, err := load(nil, base); err == nil || !strings.Contains(err.Error(), "file references") {
		t.Fatalf("load() error = %v, want file-key rejection", err)
	}
	delete(base, "THINKPIXELAG_SIGNING_KEY_ID")
	base["THINKPIXELAG_SIGNING_PRIVATE_KEY_FILE"] = "/secrets/private.pem"
	if _, err := load(nil, base); err == nil || !strings.Contains(err.Error(), "unknown environment variable") {
		t.Fatalf("load() error = %v, want unknown private-key setting", err)
	}
}

func TestSecretSafeRendering(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Database.URL = NewSecret("postgres://user:database-password@db.example/service")
	c.OPA.BearerToken = NewSecret("opa-bearer-token")
	c.Valkey.URL = NewSecret("rediss://:valkey-password@cache.example")
	c.Valkey.CacheIntegrityKey = NewSecret("cache-hmac-secret-0123456789abcdef")
	c.OIDC.IssuerURL = "https://id.example/issuer"
	c.OIDC.Audience = "thinkpixelag"

	jsonValue, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	renderings := []string{
		c.String(),
		fmt.Sprintf("%v", c),
		fmt.Sprintf("%+v", c),
		fmt.Sprintf("%#v", c),
		string(jsonValue),
		fmt.Sprintf("%v %+v %#v", c.Database.URL, c.OPA.BearerToken, c.Valkey.URL),
	}
	for _, rendering := range renderings {
		for _, secret := range []string{"database-password", "opa-bearer-token", "valkey-password", "cache-hmac-secret"} {
			if strings.Contains(rendering, secret) {
				t.Fatalf("rendering leaked %q: %s", secret, rendering)
			}
		}
	}
	if !strings.Contains(c.String(), `"url_configured":true`) || !strings.Contains(c.String(), `"bearer_token_configured":true`) {
		t.Errorf("safe rendering does not report secret presence: %s", c.String())
	}
}

func TestValidateURLs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "database scheme", mutate: func(c *Config) { c.Database.URL = NewSecret("mysql://db.example/service") }, wantErr: "database URL"},
		{name: "OPA credentials", mutate: func(c *Config) { c.OPA.URL = "https://user:secret@opa.example" }, wantErr: "OPA URL"},
		{name: "OPA query", mutate: func(c *Config) { c.OPA.URL = "https://opa.example?token=secret" }, wantErr: "OPA URL"},
		{name: "Valkey scheme", mutate: func(c *Config) { c.Valkey.URL = NewSecret("https://cache.example") }, wantErr: "Valkey URL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := Defaults()
			c.Database.URL = NewSecret("postgres://db.example/service")
			c.OIDC.IssuerURL = "https://id.example/issuer"
			c.OIDC.Audience = "thinkpixelag"
			tt.mutate(&c)
			if err := c.Validate(); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateValkeyCacheIntegrityKey(t *testing.T) {
	t.Parallel()
	c := Defaults()
	c.Database.URL = NewSecret("postgres://db.example/service")
	c.OIDC.IssuerURL = "https://id.example/issuer"
	c.OIDC.Audience = "thinkpixelag"
	c.Valkey.URL = NewSecret("redis://127.0.0.1:6379")
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "Valkey cache HMAC key") {
		t.Fatalf("Validate() error = %v, want cache HMAC failure", err)
	}
	c.Valkey.URL = Secret{}
	c.Valkey.CacheIntegrityKey = NewSecret("0123456789abcdef0123456789abcdef")
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "requires a Valkey URL") {
		t.Fatalf("Validate() error = %v, want orphan cache HMAC failure", err)
	}
}
