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
}

type HTTPConfig struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
}

type DatabaseConfig struct {
	URL            Secret
	ConnectTimeout time.Duration
}

type LogConfig struct {
	Level string `json:"level"`
}

type OPAConfig struct {
	URL          string
	DecisionPath string
	Timeout      time.Duration
	BearerToken  Secret
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
	URL     Secret
	Timeout time.Duration
}

type OIDCConfig struct {
	IssuerURL string
	Audience  string
}

// Defaults returns safe, non-secret defaults. Required trust and persistence
// settings intentionally remain empty and are rejected by Validate.
func Defaults() Config {
	return Config{
		Environment: EnvironmentLocal,
		HTTP: HTTPConfig{
			Address:           ":8080",
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			ShutdownTimeout:   20 * time.Second,
		},
		Database: DatabaseConfig{ConnectTimeout: 5 * time.Second},
		Log:      LogConfig{Level: "info"},
		OPA: OPAConfig{
			URL:          "http://127.0.0.1:8181",
			DecisionPath: "/v1/data/thinkpixelag/decision",
			Timeout:      2 * time.Second,
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
	}
}

type safeConfig struct {
	Environment Environment `json:"environment"`
	HTTP        HTTPConfig  `json:"http"`
	Database    struct {
		URLConfigured  bool          `json:"url_configured"`
		ConnectTimeout time.Duration `json:"connect_timeout"`
	} `json:"database"`
	Log LogConfig `json:"log"`
	OPA struct {
		URL                   string        `json:"url"`
		DecisionPath          string        `json:"decision_path"`
		Timeout               time.Duration `json:"timeout"`
		BearerTokenConfigured bool          `json:"bearer_token_configured"`
	} `json:"opa"`
	Telemetry TelemetryConfig `json:"telemetry"`
	Valkey    struct {
		URLConfigured bool          `json:"url_configured"`
		Timeout       time.Duration `json:"timeout"`
	} `json:"valkey"`
	OIDC OIDCConfig `json:"oidc"`
}

func (c Config) safe() safeConfig {
	var out safeConfig
	out.Environment = c.Environment
	out.HTTP = c.HTTP
	out.Database.URLConfigured = c.Database.URL.IsSet()
	out.Database.ConnectTimeout = c.Database.ConnectTimeout
	out.Log = c.Log
	out.OPA.URL = c.OPA.URL
	out.OPA.DecisionPath = c.OPA.DecisionPath
	out.OPA.Timeout = c.OPA.Timeout
	out.OPA.BearerTokenConfigured = c.OPA.BearerToken.IsSet()
	out.Telemetry = c.Telemetry
	out.Valkey.URLConfigured = c.Valkey.URL.IsSet()
	out.Valkey.Timeout = c.Valkey.Timeout
	out.OIDC = c.OIDC
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
