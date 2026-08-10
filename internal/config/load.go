package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const envPrefix = "THINKPIXELAG_"

var knownEnvironment = map[string]func(*Config, string) error{
	"THINKPIXELAG_ENVIRONMENT":              setEnvironment,
	"THINKPIXELAG_HTTP_ADDRESS":             setString(func(c *Config) *string { return &c.HTTP.Address }),
	"THINKPIXELAG_HTTP_READ_HEADER_TIMEOUT": setDuration(func(c *Config) *time.Duration { return &c.HTTP.ReadHeaderTimeout }),
	"THINKPIXELAG_HTTP_READ_TIMEOUT":        setDuration(func(c *Config) *time.Duration { return &c.HTTP.ReadTimeout }),
	"THINKPIXELAG_HTTP_WRITE_TIMEOUT":       setDuration(func(c *Config) *time.Duration { return &c.HTTP.WriteTimeout }),
	"THINKPIXELAG_HTTP_IDLE_TIMEOUT":        setDuration(func(c *Config) *time.Duration { return &c.HTTP.IdleTimeout }),
	"THINKPIXELAG_HTTP_SHUTDOWN_TIMEOUT":    setDuration(func(c *Config) *time.Duration { return &c.HTTP.ShutdownTimeout }),
	"THINKPIXELAG_DATABASE_URL":             setSecret(func(c *Config) *Secret { return &c.Database.URL }),
	"THINKPIXELAG_DATABASE_CONNECT_TIMEOUT": setDuration(func(c *Config) *time.Duration { return &c.Database.ConnectTimeout }),
	"THINKPIXELAG_OPA_URL":                  setString(func(c *Config) *string { return &c.OPA.URL }),
	"THINKPIXELAG_OPA_DECISION_PATH":        setString(func(c *Config) *string { return &c.OPA.DecisionPath }),
	"THINKPIXELAG_OPA_TIMEOUT":              setDuration(func(c *Config) *time.Duration { return &c.OPA.Timeout }),
	"THINKPIXELAG_OPA_BEARER_TOKEN":         setSecret(func(c *Config) *Secret { return &c.OPA.BearerToken }),
	"THINKPIXELAG_VALKEY_URL":               setSecret(func(c *Config) *Secret { return &c.Valkey.URL }),
	"THINKPIXELAG_VALKEY_TIMEOUT":           setDuration(func(c *Config) *time.Duration { return &c.Valkey.Timeout }),
	"THINKPIXELAG_OIDC_ISSUER_URL":          setString(func(c *Config) *string { return &c.OIDC.IssuerURL }),
	"THINKPIXELAG_OIDC_AUDIENCE":            setString(func(c *Config) *string { return &c.OIDC.Audience }),
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
	fs.DurationVar(&c.HTTP.ReadHeaderTimeout, "http-read-header-timeout", c.HTTP.ReadHeaderTimeout, "HTTP header read timeout")
	fs.DurationVar(&c.HTTP.ReadTimeout, "http-read-timeout", c.HTTP.ReadTimeout, "HTTP request read timeout")
	fs.DurationVar(&c.HTTP.WriteTimeout, "http-write-timeout", c.HTTP.WriteTimeout, "HTTP response write timeout")
	fs.DurationVar(&c.HTTP.IdleTimeout, "http-idle-timeout", c.HTTP.IdleTimeout, "HTTP idle timeout")
	fs.DurationVar(&c.HTTP.ShutdownTimeout, "http-shutdown-timeout", c.HTTP.ShutdownTimeout, "graceful shutdown timeout")
	fs.DurationVar(&c.Database.ConnectTimeout, "database-connect-timeout", c.Database.ConnectTimeout, "database connection timeout")
	fs.StringVar(&c.OPA.URL, "opa-url", c.OPA.URL, "OPA base URL")
	fs.StringVar(&c.OPA.DecisionPath, "opa-decision-path", c.OPA.DecisionPath, "OPA decision document path")
	fs.DurationVar(&c.OPA.Timeout, "opa-timeout", c.OPA.Timeout, "OPA request timeout")
	fs.DurationVar(&c.Valkey.Timeout, "valkey-timeout", c.Valkey.Timeout, "Valkey request timeout")
	fs.StringVar(&c.OIDC.IssuerURL, "oidc-issuer-url", c.OIDC.IssuerURL, "trusted OIDC issuer URL")
	fs.StringVar(&c.OIDC.Audience, "oidc-audience", c.OIDC.Audience, "required OIDC audience")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	return nil
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
