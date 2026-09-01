// Package config loads and validates process configuration without exposing
// secret values through ordinary formatting or JSON serialization.
package config

import (
	"encoding/json"
	"fmt"
	"time"
)

const redacted = "[REDACTED]"

// Environment identifies the deployment posture used for validation.
type Environment string

const (
	EnvironmentLocal      Environment = "local"
	EnvironmentTest       Environment = "test"
	EnvironmentProduction Environment = "production"
)

// Secret stores sensitive configuration. Its value is available only through
// Value; all standard string and JSON representations are redacted.
type Secret struct{ value string }

// NewSecret wraps a sensitive value.
func NewSecret(value string) Secret { return Secret{value: value} }

// Value returns the sensitive value for use by the owning adapter.
func (s Secret) Value() string { return s.value }

// IsSet reports whether a non-empty sensitive value was configured.
func (s Secret) IsSet() bool { return s.value != "" }

// String implements fmt.Stringer without exposing the value.
func (s Secret) String() string { return redacted }

// GoString implements fmt.GoStringer without exposing the value.
func (s Secret) GoString() string { return redacted }

// MarshalJSON prevents accidental disclosure in structured output.
func (s Secret) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

// Config is the validated configuration for the governance-plane process.
type Config struct {
	Environment Environment
	HTTP        HTTPConfig
	Database    DatabaseConfig
	Log         LogConfig
	OPA         OPAConfig
	Telemetry   TelemetryConfig
	Valkey      ValkeyConfig
	OIDC        OIDCConfig
	Signing     SigningConfig
	Evidence    EvidenceConfig
}

type HTTPConfig struct {
	Address           string
	MaxHeaderBytes    int
	MaxBodyBytes      int64
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	HandlerTimeout    time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

type DatabaseConfig struct {
	URL                   Secret
	ConnectTimeout        time.Duration
	HealthTimeout         time.Duration
	StatementTimeout      time.Duration
	LockTimeout           time.Duration
	MaxConnectionLifetime time.Duration
	MaxConnectionIdleTime time.Duration
	MinConnections        int32
	MaxConnections        int32
}

type LogConfig struct {
	Level string `json:"level"`
}

type OPAConfig struct {
	URL            string
	DecisionPath   string
	Timeout        time.Duration
	DecisionMaxTTL time.Duration
	BundleMaxAge   time.Duration
	BearerToken    Secret
}

type TelemetryConfig struct {
	MetricsEnabled     bool
	TracingMode        string
	ServiceName        string
	OTLPEndpoint       string
	TraceSampleRatio   float64
	TraceExportTimeout time.Duration
	TraceBatchTimeout  time.Duration
}

type ValkeyConfig struct {
	URL               Secret
	CacheIntegrityKey Secret
	Timeout           time.Duration
}

type OIDCConfig struct {
	IssuerURL        string
	Audience         string
	Algorithms       string
	TenantClaim      string
	RolesClaim       string
	RoleMappings     string
	DiscoveryTimeout time.Duration
	JWKSMinTTL       time.Duration
	JWKSMaxTTL       time.Duration
	JWKSStaleTTL     time.Duration
	ClockSkew        time.Duration
	MaxTokenAge      time.Duration
}

// SigningConfig contains only a managed provider and opaque key reference.
// Private-key bytes and filesystem paths are deliberately not configurable.
type SigningConfig struct {
	Provider  string
	KeyID     string
	Algorithm string
}

// EvidenceConfig identifies an independently administered authenticated sink.
// The bearer token is redacted by the same Secret boundary as other credentials.
type EvidenceConfig struct {
	SinkID           string
	Endpoint         string
	BearerToken      Secret
	Timeout          time.Duration
	MaxResponseBytes int64
}

// Defaults returns safe, non-secret defaults. Required trust and persistence
// settings intentionally remain empty and are rejected by Validate.
func Defaults() Config {
	return Config{
		Environment: EnvironmentLocal,
		HTTP: HTTPConfig{
			Address:           ":8080",
			MaxHeaderBytes:    1 << 20,
			MaxBodyBytes:      1 << 20,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			HandlerTimeout:    15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			ShutdownTimeout:   20 * time.Second,
		},
		Database: DatabaseConfig{
			ConnectTimeout: 5 * time.Second, HealthTimeout: 2 * time.Second,
			StatementTimeout: 10 * time.Second, LockTimeout: 2 * time.Second,
			MaxConnectionLifetime: 30 * time.Minute, MaxConnectionIdleTime: 5 * time.Minute,
			MinConnections: 1, MaxConnections: 20,
		},
		Log: LogConfig{Level: "info"},
		OPA: OPAConfig{
			URL:            "http://127.0.0.1:8181",
			DecisionPath:   "/v1/data/thinkpixelag/authorization/decision",
			Timeout:        2 * time.Second,
			DecisionMaxTTL: 30 * time.Second,
			BundleMaxAge:   5 * time.Minute,
		},
		Telemetry: TelemetryConfig{
			MetricsEnabled:     true,
			TracingMode:        "noop",
			ServiceName:        "thinkpixelag",
			OTLPEndpoint:       "http://127.0.0.1:4318",
			TraceSampleRatio:   1,
			TraceExportTimeout: 5 * time.Second,
			TraceBatchTimeout:  5 * time.Second,
		},
		Valkey: ValkeyConfig{Timeout: 500 * time.Millisecond},
		OIDC: OIDCConfig{
			Algorithms: "RS256", TenantClaim: "tenant_id", RolesClaim: "roles",
			DiscoveryTimeout: 5 * time.Second, JWKSMinTTL: time.Minute,
			JWKSMaxTTL: time.Hour, JWKSStaleTTL: 6 * time.Hour,
			ClockSkew: 30 * time.Second, MaxTokenAge: 24 * time.Hour,
		},
		Signing:  SigningConfig{Provider: "disabled", Algorithm: "ED25519"},
		Evidence: EvidenceConfig{Timeout: 5 * time.Second, MaxResponseBytes: 64 << 10},
	}
}

type safeConfig struct {
	Environment Environment `json:"environment"`
	HTTP        HTTPConfig  `json:"http"`
	Database    struct {
		URLConfigured         bool          `json:"url_configured"`
		ConnectTimeout        time.Duration `json:"connect_timeout"`
		HealthTimeout         time.Duration `json:"health_timeout"`
		StatementTimeout      time.Duration `json:"statement_timeout"`
		LockTimeout           time.Duration `json:"lock_timeout"`
		MaxConnectionLifetime time.Duration `json:"max_connection_lifetime"`
		MaxConnectionIdleTime time.Duration `json:"max_connection_idle_time"`
		MinConnections        int32         `json:"min_connections"`
		MaxConnections        int32         `json:"max_connections"`
	} `json:"database"`
	Log LogConfig `json:"log"`
	OPA struct {
		URL                   string        `json:"url"`
		DecisionPath          string        `json:"decision_path"`
		Timeout               time.Duration `json:"timeout"`
		DecisionMaxTTL        time.Duration `json:"decision_max_ttl"`
		BundleMaxAge          time.Duration `json:"bundle_max_age"`
		BearerTokenConfigured bool          `json:"bearer_token_configured"`
	} `json:"opa"`
	Telemetry TelemetryConfig `json:"telemetry"`
	Valkey    struct {
		URLConfigured               bool          `json:"url_configured"`
		CacheIntegrityKeyConfigured bool          `json:"cache_integrity_key_configured"`
		Timeout                     time.Duration `json:"timeout"`
	} `json:"valkey"`
	OIDC     OIDCConfig    `json:"oidc"`
	Signing  SigningConfig `json:"signing"`
	Evidence struct {
		SinkID                string        `json:"sink_id"`
		Endpoint              string        `json:"endpoint"`
		BearerTokenConfigured bool          `json:"bearer_token_configured"`
		Timeout               time.Duration `json:"timeout"`
		MaxResponseBytes      int64         `json:"max_response_bytes"`
	} `json:"evidence"`
}

func (c Config) safe() safeConfig {
	var out safeConfig
	out.Environment = c.Environment
	out.HTTP = c.HTTP
	out.Database.URLConfigured = c.Database.URL.IsSet()
	out.Database.ConnectTimeout = c.Database.ConnectTimeout
	out.Database.HealthTimeout = c.Database.HealthTimeout
	out.Database.StatementTimeout = c.Database.StatementTimeout
	out.Database.LockTimeout = c.Database.LockTimeout
	out.Database.MaxConnectionLifetime = c.Database.MaxConnectionLifetime
	out.Database.MaxConnectionIdleTime = c.Database.MaxConnectionIdleTime
	out.Database.MinConnections = c.Database.MinConnections
	out.Database.MaxConnections = c.Database.MaxConnections
	out.Log = c.Log
	out.OPA.URL = c.OPA.URL
	out.OPA.DecisionPath = c.OPA.DecisionPath
	out.OPA.Timeout = c.OPA.Timeout
	out.OPA.DecisionMaxTTL = c.OPA.DecisionMaxTTL
	out.OPA.BundleMaxAge = c.OPA.BundleMaxAge
	out.OPA.BearerTokenConfigured = c.OPA.BearerToken.IsSet()
	out.Telemetry = c.Telemetry
	out.Valkey.URLConfigured = c.Valkey.URL.IsSet()
	out.Valkey.CacheIntegrityKeyConfigured = c.Valkey.CacheIntegrityKey.IsSet()
	out.Valkey.Timeout = c.Valkey.Timeout
	out.OIDC = c.OIDC
	out.Signing = c.Signing
	out.Evidence.SinkID = c.Evidence.SinkID
	out.Evidence.Endpoint = c.Evidence.Endpoint
	out.Evidence.BearerTokenConfigured = c.Evidence.BearerToken.IsSet()
	out.Evidence.Timeout = c.Evidence.Timeout
	out.Evidence.MaxResponseBytes = c.Evidence.MaxResponseBytes
	return out
}

// MarshalJSON returns a stable, secret-safe configuration representation.
func (c Config) MarshalJSON() ([]byte, error) { return json.Marshal(c.safe()) }

// String returns compact, secret-safe JSON suitable for startup logging.
func (c Config) String() string {
	b, err := c.MarshalJSON()
	if err != nil {
		return `{"configuration":"unavailable"}`
	}
	return string(b)
}

// GoString prevents %#v formatting from traversing secret-bearing fields.
func (c Config) GoString() string { return c.String() }

var _ fmt.Stringer = Config{}
