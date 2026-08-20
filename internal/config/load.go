package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const envPrefix = "THINKPIXELAG_"

var knownEnvironment = map[string]func(*Config, string) error{
	"THINKPIXELAG_ENVIRONMENT":                       setEnvironment,
	"THINKPIXELAG_HTTP_ADDRESS":                      setString(func(c *Config) *string { return &c.HTTP.Address }),
	"THINKPIXELAG_HTTP_MAX_HEADER_BYTES":             setInt(func(c *Config) *int { return &c.HTTP.MaxHeaderBytes }),
	"THINKPIXELAG_HTTP_MAX_BODY_BYTES":               setInt64(func(c *Config) *int64 { return &c.HTTP.MaxBodyBytes }),
	"THINKPIXELAG_HTTP_READ_HEADER_TIMEOUT":          setDuration(func(c *Config) *time.Duration { return &c.HTTP.ReadHeaderTimeout }),
	"THINKPIXELAG_HTTP_READ_TIMEOUT":                 setDuration(func(c *Config) *time.Duration { return &c.HTTP.ReadTimeout }),
	"THINKPIXELAG_HTTP_HANDLER_TIMEOUT":              setDuration(func(c *Config) *time.Duration { return &c.HTTP.HandlerTimeout }),
	"THINKPIXELAG_HTTP_WRITE_TIMEOUT":                setDuration(func(c *Config) *time.Duration { return &c.HTTP.WriteTimeout }),
	"THINKPIXELAG_HTTP_IDLE_TIMEOUT":                 setDuration(func(c *Config) *time.Duration { return &c.HTTP.IdleTimeout }),
	"THINKPIXELAG_HTTP_SHUTDOWN_TIMEOUT":             setDuration(func(c *Config) *time.Duration { return &c.HTTP.ShutdownTimeout }),
	"THINKPIXELAG_DATABASE_URL":                      setSecret(func(c *Config) *Secret { return &c.Database.URL }),
	"THINKPIXELAG_DATABASE_CONNECT_TIMEOUT":          setDuration(func(c *Config) *time.Duration { return &c.Database.ConnectTimeout }),
	"THINKPIXELAG_DATABASE_HEALTH_TIMEOUT":           setDuration(func(c *Config) *time.Duration { return &c.Database.HealthTimeout }),
	"THINKPIXELAG_DATABASE_STATEMENT_TIMEOUT":        setDuration(func(c *Config) *time.Duration { return &c.Database.StatementTimeout }),
	"THINKPIXELAG_DATABASE_LOCK_TIMEOUT":             setDuration(func(c *Config) *time.Duration { return &c.Database.LockTimeout }),
	"THINKPIXELAG_DATABASE_MAX_CONNECTION_LIFETIME":  setDuration(func(c *Config) *time.Duration { return &c.Database.MaxConnectionLifetime }),
	"THINKPIXELAG_DATABASE_MAX_CONNECTION_IDLE_TIME": setDuration(func(c *Config) *time.Duration { return &c.Database.MaxConnectionIdleTime }),
	"THINKPIXELAG_DATABASE_MIN_CONNECTIONS":          setInt32(func(c *Config) *int32 { return &c.Database.MinConnections }),
	"THINKPIXELAG_DATABASE_MAX_CONNECTIONS":          setInt32(func(c *Config) *int32 { return &c.Database.MaxConnections }),
	"THINKPIXELAG_LOG_LEVEL":                         setString(func(c *Config) *string { return &c.Log.Level }),
	"THINKPIXELAG_OPA_URL":                           setString(func(c *Config) *string { return &c.OPA.URL }),
	"THINKPIXELAG_OPA_DECISION_PATH":                 setString(func(c *Config) *string { return &c.OPA.DecisionPath }),
	"THINKPIXELAG_OPA_TIMEOUT":                       setDuration(func(c *Config) *time.Duration { return &c.OPA.Timeout }),
	"THINKPIXELAG_OPA_BEARER_TOKEN":                  setSecret(func(c *Config) *Secret { return &c.OPA.BearerToken }),
	"THINKPIXELAG_METRICS_ENABLED":                   setBool(func(c *Config) *bool { return &c.Telemetry.MetricsEnabled }),
	"THINKPIXELAG_TRACING_MODE":                      setString(func(c *Config) *string { return &c.Telemetry.TracingMode }),
	"THINKPIXELAG_SERVICE_NAME":                      setString(func(c *Config) *string { return &c.Telemetry.ServiceName }),
	"THINKPIXELAG_OTLP_ENDPOINT":                     setString(func(c *Config) *string { return &c.Telemetry.OTLPEndpoint }),
	"THINKPIXELAG_TRACE_SAMPLE_RATIO":                setFloat64(func(c *Config) *float64 { return &c.Telemetry.TraceSampleRatio }),
	"THINKPIXELAG_TRACE_EXPORT_TIMEOUT":              setDuration(func(c *Config) *time.Duration { return &c.Telemetry.TraceExportTimeout }),
	"THINKPIXELAG_TRACE_BATCH_TIMEOUT":               setDuration(func(c *Config) *time.Duration { return &c.Telemetry.TraceBatchTimeout }),
	"THINKPIXELAG_VALKEY_URL":                        setSecret(func(c *Config) *Secret { return &c.Valkey.URL }),
	"THINKPIXELAG_VALKEY_TIMEOUT":                    setDuration(func(c *Config) *time.Duration { return &c.Valkey.Timeout }),
	"THINKPIXELAG_OIDC_ISSUER_URL":                   setString(func(c *Config) *string { return &c.OIDC.IssuerURL }),
	"THINKPIXELAG_OIDC_AUDIENCE":                     setString(func(c *Config) *string { return &c.OIDC.Audience }),
	"THINKPIXELAG_OIDC_ALGORITHMS":                   setString(func(c *Config) *string { return &c.OIDC.Algorithms }),
	"THINKPIXELAG_OIDC_TENANT_CLAIM":                 setString(func(c *Config) *string { return &c.OIDC.TenantClaim }),
	"THINKPIXELAG_OIDC_ROLES_CLAIM":                  setString(func(c *Config) *string { return &c.OIDC.RolesClaim }),
	"THINKPIXELAG_OIDC_ROLE_MAPPINGS":                setString(func(c *Config) *string { return &c.OIDC.RoleMappings }),
	"THINKPIXELAG_OIDC_DISCOVERY_TIMEOUT":            setDuration(func(c *Config) *time.Duration { return &c.OIDC.DiscoveryTimeout }),
	"THINKPIXELAG_OIDC_JWKS_MIN_TTL":                 setDuration(func(c *Config) *time.Duration { return &c.OIDC.JWKSMinTTL }),
	"THINKPIXELAG_OIDC_JWKS_MAX_TTL":                 setDuration(func(c *Config) *time.Duration { return &c.OIDC.JWKSMaxTTL }),
	"THINKPIXELAG_OIDC_JWKS_STALE_TTL":               setDuration(func(c *Config) *time.Duration { return &c.OIDC.JWKSStaleTTL }),
	"THINKPIXELAG_OIDC_CLOCK_SKEW":                   setDuration(func(c *Config) *time.Duration { return &c.OIDC.ClockSkew }),
	"THINKPIXELAG_OIDC_MAX_TOKEN_AGE":                setDuration(func(c *Config) *time.Duration { return &c.OIDC.MaxTokenAge }),
}

// Load reads process environment and command-line arguments, then validates the
// result. Precedence is defaults, environment, flags. Unknown prefixed
// environment variables and unknown/positional flags are rejected.
func Load(args []string) (Config, error) {
	return load(args, environmentMap(os.Environ()))
}

func load(args []string, environment map[string]string) (Config, error) {
	c := Defaults()
	if err := applyEnvironment(&c, environment); err != nil {
		return Config{}, err
	}
	if err := applyFlags(&c, args); err != nil {
		return Config{}, err
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func environmentMap(entries []string) map[string]string {
	result := make(map[string]string)
	for _, entry := range entries {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			result[name] = value
		}
	}
	return result
}

func applyEnvironment(c *Config, environment map[string]string) error {
	var problems []string
	for name, value := range environment {
		setter, known := knownEnvironment[name]
		if !known {
			if strings.HasPrefix(name, envPrefix) {
				problems = append(problems, fmt.Sprintf("unknown environment variable %s", name))
			}
			continue
		}
		if err := setter(c, value); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(problems) != 0 {
		return newValidationError(problems)
	}
	return nil
}

func applyFlags(c *Config, args []string) error {
	fs := flag.NewFlagSet("thinkpixelag", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.Var((*environmentValue)(&c.Environment), "environment", "deployment posture: local, test, or production")
	fs.StringVar(&c.HTTP.Address, "http-address", c.HTTP.Address, "HTTP listen address")
	fs.IntVar(&c.HTTP.MaxHeaderBytes, "http-max-header-bytes", c.HTTP.MaxHeaderBytes, "maximum HTTP request header bytes")
	fs.Int64Var(&c.HTTP.MaxBodyBytes, "http-max-body-bytes", c.HTTP.MaxBodyBytes, "maximum HTTP request body bytes")
	fs.DurationVar(&c.HTTP.ReadHeaderTimeout, "http-read-header-timeout", c.HTTP.ReadHeaderTimeout, "HTTP header read timeout")
	fs.DurationVar(&c.HTTP.ReadTimeout, "http-read-timeout", c.HTTP.ReadTimeout, "HTTP request read timeout")
	fs.DurationVar(&c.HTTP.HandlerTimeout, "http-handler-timeout", c.HTTP.HandlerTimeout, "HTTP handler deadline")
	fs.DurationVar(&c.HTTP.WriteTimeout, "http-write-timeout", c.HTTP.WriteTimeout, "HTTP response write timeout")
	fs.DurationVar(&c.HTTP.IdleTimeout, "http-idle-timeout", c.HTTP.IdleTimeout, "HTTP idle timeout")
	fs.DurationVar(&c.HTTP.ShutdownTimeout, "http-shutdown-timeout", c.HTTP.ShutdownTimeout, "graceful shutdown timeout")
	fs.DurationVar(&c.Database.ConnectTimeout, "database-connect-timeout", c.Database.ConnectTimeout, "database connection timeout")
	fs.DurationVar(&c.Database.HealthTimeout, "database-health-timeout", c.Database.HealthTimeout, "database health-check timeout")
	fs.DurationVar(&c.Database.StatementTimeout, "database-statement-timeout", c.Database.StatementTimeout, "database statement timeout")
	fs.DurationVar(&c.Database.LockTimeout, "database-lock-timeout", c.Database.LockTimeout, "database lock timeout")
	fs.DurationVar(&c.Database.MaxConnectionLifetime, "database-max-connection-lifetime", c.Database.MaxConnectionLifetime, "maximum database connection lifetime")
	fs.DurationVar(&c.Database.MaxConnectionIdleTime, "database-max-connection-idle-time", c.Database.MaxConnectionIdleTime, "maximum database connection idle time")
	fs.Var(newInt32Value(&c.Database.MinConnections), "database-min-connections", "minimum database pool connections")
	fs.Var(newInt32Value(&c.Database.MaxConnections), "database-max-connections", "maximum database pool connections")
	fs.StringVar(&c.Log.Level, "log-level", c.Log.Level, "minimum log level: debug, info, warn, or error")
	fs.StringVar(&c.OPA.URL, "opa-url", c.OPA.URL, "OPA base URL")
	fs.StringVar(&c.OPA.DecisionPath, "opa-decision-path", c.OPA.DecisionPath, "OPA decision document path")
	fs.DurationVar(&c.OPA.Timeout, "opa-timeout", c.OPA.Timeout, "OPA request timeout")
	fs.BoolVar(&c.Telemetry.MetricsEnabled, "metrics-enabled", c.Telemetry.MetricsEnabled, "enable Prometheus metrics")
	fs.StringVar(&c.Telemetry.TracingMode, "tracing-mode", c.Telemetry.TracingMode, "tracing mode: noop or otlp")
	fs.StringVar(&c.Telemetry.ServiceName, "service-name", c.Telemetry.ServiceName, "OpenTelemetry service name")
	fs.StringVar(&c.Telemetry.OTLPEndpoint, "otlp-endpoint", c.Telemetry.OTLPEndpoint, "OTLP/HTTP collector base URL")
	fs.Float64Var(&c.Telemetry.TraceSampleRatio, "trace-sample-ratio", c.Telemetry.TraceSampleRatio, "root trace sampling ratio from 0 through 1")
	fs.DurationVar(&c.Telemetry.TraceExportTimeout, "trace-export-timeout", c.Telemetry.TraceExportTimeout, "OTLP export timeout")
	fs.DurationVar(&c.Telemetry.TraceBatchTimeout, "trace-batch-timeout", c.Telemetry.TraceBatchTimeout, "maximum trace batch delay")
	fs.DurationVar(&c.Valkey.Timeout, "valkey-timeout", c.Valkey.Timeout, "Valkey request timeout")
	fs.StringVar(&c.OIDC.IssuerURL, "oidc-issuer-url", c.OIDC.IssuerURL, "trusted OIDC issuer URL")
	fs.StringVar(&c.OIDC.Audience, "oidc-audience", c.OIDC.Audience, "required OIDC audience")
	fs.StringVar(&c.OIDC.Algorithms, "oidc-algorithms", c.OIDC.Algorithms, "comma-separated allowed JWT algorithms")
	fs.StringVar(&c.OIDC.TenantClaim, "oidc-tenant-claim", c.OIDC.TenantClaim, "verified tenant claim name")
	fs.StringVar(&c.OIDC.RolesClaim, "oidc-roles-claim", c.OIDC.RolesClaim, "verified roles claim name")
	fs.StringVar(&c.OIDC.RoleMappings, "oidc-role-mappings", c.OIDC.RoleMappings, "comma-separated external=internal role mappings")
	fs.DurationVar(&c.OIDC.DiscoveryTimeout, "oidc-discovery-timeout", c.OIDC.DiscoveryTimeout, "OIDC discovery/JWKS timeout")
	fs.DurationVar(&c.OIDC.JWKSMinTTL, "oidc-jwks-min-ttl", c.OIDC.JWKSMinTTL, "minimum JWKS cache TTL")
	fs.DurationVar(&c.OIDC.JWKSMaxTTL, "oidc-jwks-max-ttl", c.OIDC.JWKSMaxTTL, "maximum JWKS cache TTL")
	fs.DurationVar(&c.OIDC.JWKSStaleTTL, "oidc-jwks-stale-ttl", c.OIDC.JWKSStaleTTL, "maximum cached-key outage allowance")
	fs.DurationVar(&c.OIDC.ClockSkew, "oidc-clock-skew", c.OIDC.ClockSkew, "JWT time validation clock skew")
	fs.DurationVar(&c.OIDC.MaxTokenAge, "oidc-max-token-age", c.OIDC.MaxTokenAge, "maximum JWT lifetime")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	return nil
}

type int32Value int32

func newInt32Value(target *int32) *int32Value { return (*int32Value)(target) }
func (v *int32Value) String() string          { return strconv.FormatInt(int64(*v), 10) }
func (v *int32Value) Set(value string) error {
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return fmt.Errorf("must be a 32-bit integer")
	}
	*v = int32Value(parsed)
	return nil
}

func setInt32(field func(*Config) *int32) func(*Config, string) error {
	return func(c *Config, value string) error { return newInt32Value(field(c)).Set(value) }
}

type environmentValue Environment

func (v *environmentValue) String() string { return string(*v) }

func (v *environmentValue) Set(value string) error {
	*v = environmentValue(value)
	return nil
}

func setEnvironment(c *Config, value string) error {
	c.Environment = Environment(value)
	return nil
}

func setString(destination func(*Config) *string) func(*Config, string) error {
	return func(c *Config, value string) error {
		*destination(c) = value
		return nil
	}
}

func setSecret(destination func(*Config) *Secret) func(*Config, string) error {
	return func(c *Config, value string) error {
		*destination(c) = NewSecret(value)
		return nil
	}
}

func setDuration(destination func(*Config) *time.Duration) func(*Config, string) error {
	return func(c *Config, value string) error {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return errors.New("must be a Go duration such as 500ms or 5s")
		}
		*destination(c) = parsed
		return nil
	}
}

func setBool(destination func(*Config) *bool) func(*Config, string) error {
	return func(c *Config, value string) error {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return errors.New("must be true or false")
		}
		*destination(c) = parsed
		return nil
	}
}

func setFloat64(destination func(*Config) *float64) func(*Config, string) error {
	return func(c *Config, value string) error {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return errors.New("must be a number")
		}
		*destination(c) = parsed
		return nil
	}
}

func setInt(destination func(*Config) *int) func(*Config, string) error {
	return func(c *Config, value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return errors.New("must be an integer")
		}
		*destination(c) = parsed
		return nil
	}
}

func setInt64(destination func(*Config) *int64) func(*Config, string) error {
	return func(c *Config, value string) error {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return errors.New("must be an integer")
		}
		*destination(c) = parsed
		return nil
	}
}
