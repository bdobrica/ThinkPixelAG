# Configuration Reference

ThinkPixelAG configuration is typed, validated before startup, and loaded with
this precedence:

1. safe built-in defaults;
2. `THINKPIXELAG_*` environment variables;
3. non-secret command-line flags.

Unknown `THINKPIXELAG_*` names, unknown flags, positional arguments, invalid
types, missing required settings, unsafe URL forms, and out-of-range durations
are startup errors. Validation reports all independently detectable semantic
problems together. It does not contact dependencies; readiness performs runtime
dependency checks once those adapters exist.

## Settings

| Environment variable | Flag | Default | Required / validation |
|---|---|---|---|
| `THINKPIXELAG_ENVIRONMENT` | `--environment` | `local` | `local`, `test`, or `production` |
| `THINKPIXELAG_HTTP_ADDRESS` | `--http-address` | `:8080` | valid `host:port` |
| `THINKPIXELAG_HTTP_MAX_HEADER_BYTES` | `--http-max-header-bytes` | `1048576` | 1 KiB through 16 MiB |
| `THINKPIXELAG_HTTP_MAX_BODY_BYTES` | `--http-max-body-bytes` | `1048576` | 1 byte through 64 MiB; handlers may impose smaller endpoint limits |
| `THINKPIXELAG_HTTP_READ_HEADER_TIMEOUT` | `--http-read-header-timeout` | `5s` | greater than zero, at most `5m` |
| `THINKPIXELAG_HTTP_READ_TIMEOUT` | `--http-read-timeout` | `15s` | greater than zero, at most `5m` |
| `THINKPIXELAG_HTTP_HANDLER_TIMEOUT` | `--http-handler-timeout` | `15s` | context deadline; greater than zero, at most `5m` |
| `THINKPIXELAG_HTTP_WRITE_TIMEOUT` | `--http-write-timeout` | `30s` | greater than zero, at most `5m` |
| `THINKPIXELAG_HTTP_IDLE_TIMEOUT` | `--http-idle-timeout` | `1m` | greater than zero, at most `5m` |
| `THINKPIXELAG_HTTP_SHUTDOWN_TIMEOUT` | `--http-shutdown-timeout` | `20s` | greater than zero, at most `5m` |
| `THINKPIXELAG_DATABASE_URL` | none | none | required; `postgres://` or `postgresql://` |
| `THINKPIXELAG_DATABASE_CONNECT_TIMEOUT` | `--database-connect-timeout` | `5s` | greater than zero, at most `5m` |
| `THINKPIXELAG_DATABASE_HEALTH_TIMEOUT` | `--database-health-timeout` | `2s` | dependency probe deadline |
| `THINKPIXELAG_DATABASE_STATEMENT_TIMEOUT` | `--database-statement-timeout` | `10s` | server-enforced statement deadline |
| `THINKPIXELAG_DATABASE_LOCK_TIMEOUT` | `--database-lock-timeout` | `2s` | server-enforced lock wait deadline |
| `THINKPIXELAG_DATABASE_MAX_CONNECTION_LIFETIME` | `--database-max-connection-lifetime` | `30m` | greater than zero, at most `24h` |
| `THINKPIXELAG_DATABASE_MAX_CONNECTION_IDLE_TIME` | `--database-max-connection-idle-time` | `5m` | greater than zero, at most `24h` |
| `THINKPIXELAG_DATABASE_MIN_CONNECTIONS` | `--database-min-connections` | `1` | from zero through max connections |
| `THINKPIXELAG_DATABASE_MAX_CONNECTIONS` | `--database-max-connections` | `20` | from one through 1000 |
| `THINKPIXELAG_LOG_LEVEL` | `--log-level` | `info` | `debug`, `info`, `warn`, or `error` |
| `THINKPIXELAG_OPA_URL` | `--opa-url` | `http://127.0.0.1:8181` | absolute HTTP(S), no credentials/query/fragment; production requires HTTPS except loopback |
| `THINKPIXELAG_OPA_DECISION_PATH` | `--opa-decision-path` | `/v1/data/thinkpixelag/decision` | absolute path without query/fragment |
| `THINKPIXELAG_OPA_TIMEOUT` | `--opa-timeout` | `2s` | greater than zero, at most `5m` |
| `THINKPIXELAG_OPA_BEARER_TOKEN` | none | unset | optional secret |
| `THINKPIXELAG_METRICS_ENABLED` | `--metrics-enabled` | `true` | boolean; disabled mode exposes an empty private registry |
| `THINKPIXELAG_TRACING_MODE` | `--tracing-mode` | `noop` | `noop` or `otlp` |
| `THINKPIXELAG_SERVICE_NAME` | `--service-name` | `thinkpixelag` | 1–64 bytes, no surrounding whitespace/control characters |
| `THINKPIXELAG_OTLP_ENDPOINT` | `--otlp-endpoint` | `http://127.0.0.1:4318` | collector base HTTP(S) URL; production requires HTTPS except loopback |
| `THINKPIXELAG_TRACE_SAMPLE_RATIO` | `--trace-sample-ratio` | `1` | finite number from `0` through `1` |
| `THINKPIXELAG_TRACE_EXPORT_TIMEOUT` | `--trace-export-timeout` | `5s` | greater than zero, at most `5m` |
| `THINKPIXELAG_TRACE_BATCH_TIMEOUT` | `--trace-batch-timeout` | `5s` | greater than zero, at most `5m` |
| `THINKPIXELAG_VALKEY_URL` | none | unset | optional `redis://` or `rediss://`; production requires TLS except loopback |
| `THINKPIXELAG_VALKEY_TIMEOUT` | `--valkey-timeout` | `500ms` | greater than zero, at most `5m` |
| `THINKPIXELAG_OIDC_ISSUER_URL` | `--oidc-issuer-url` | none | required absolute HTTP(S), no credentials/query/fragment; HTTPS except loopback |
| `THINKPIXELAG_OIDC_AUDIENCE` | `--oidc-audience` | none | required, no surrounding whitespace or control characters |
| `THINKPIXELAG_OIDC_ALGORITHMS` | `--oidc-algorithms` | `RS256` | unique subset of `RS256,ES256`; never inferred from a token |
| `THINKPIXELAG_OIDC_TENANT_CLAIM` | `--oidc-tenant-claim` | `tenant_id` | safe authoritative tenant claim name |
| `THINKPIXELAG_OIDC_ROLES_CLAIM` | `--oidc-roles-claim` | `roles` | safe claim name distinct from tenant claim |
| `THINKPIXELAG_OIDC_ROLE_MAPPINGS` | `--oidc-role-mappings` | empty | comma-separated `external=internal`; unmapped roles are discarded |
| `THINKPIXELAG_OIDC_DISCOVERY_TIMEOUT` | `--oidc-discovery-timeout` | `5s` | discovery/JWKS deadline; greater than zero, at most `5m` |
| `THINKPIXELAG_OIDC_JWKS_MIN_TTL` | `--oidc-jwks-min-ttl` | `1m` | lower bound for provider cache advice |
| `THINKPIXELAG_OIDC_JWKS_MAX_TTL` | `--oidc-jwks-max-ttl` | `1h` | upper bound for provider cache advice |
| `THINKPIXELAG_OIDC_JWKS_STALE_TTL` | `--oidc-jwks-stale-ttl` | `6h` | final known-key outage bound |
| `THINKPIXELAG_OIDC_CLOCK_SKEW` | `--oidc-clock-skew` | `30s` | zero through `5m` |
| `THINKPIXELAG_OIDC_MAX_TOKEN_AGE` | `--oidc-max-token-age` | `24h` | maximum `exp-iat`, at most seven days |

Durations use Go syntax such as `500ms`, `5s`, or `2m`.

## Secret handling

Database and Valkey URLs and the OPA bearer token are environment-only. They are
not flags because process argument lists are commonly visible to other tooling.
In Kubernetes, populate these variables through `Secret` references or workload
identity integrations; never place values in a committed ConfigMap or manifest.

The typed `Secret` wrapper and top-level configuration implement redacted
ordinary, Go-syntax, and JSON rendering. Safe startup rendering reports only
whether a secret is configured. Code must not log the result of `Secret.Value()`
or copy secret values into errors, metrics, traces, or policy input.

Issuer and OPA endpoint URLs are operational metadata rather than credentials,
but URL user information, queries, and fragments are rejected so tokens cannot
be smuggled into values that are safe to render. Database and Valkey URLs are
treated as entirely secret even when they contain no inline password.

## Minimal local example

```sh
export THINKPIXELAG_DATABASE_URL='postgresql://thinkpixelag_local:thinkpixelag_local_only_change_me@127.0.0.1:5432/thinkpixelag_local?sslmode=disable'
export THINKPIXELAG_OPA_URL='http://127.0.0.1:8181'
# Only after `make dev-up-valkey`:
export THINKPIXELAG_VALKEY_URL='redis://:thinkpixelag_valkey_local_only_change_me@127.0.0.1:6379/0'
export THINKPIXELAG_OIDC_ISSUER_URL='http://127.0.0.1:5556/dex'
export THINKPIXELAG_OIDC_AUDIENCE='thinkpixelag-local'
```

These credentials match `compose.yaml` and are for isolated local development
only. Start them with `make dev-up` or `make dev-up-valkey`. If `.env` overrides
a published port, update the corresponding URL. Production identity, database,
OPA, and optional Valkey configuration must use managed secret delivery and
encrypted transport as described above.

OIDC discovery is limited to the configured issuer and its `jwks_uri` must use
the same scheme and authority. HTTPS is mandatory except for numeric loopback
development endpoints. Protected handlers accept only an Authorization bearer
token; tenant/principal/role forwarding headers and request tenant fields are
rejected. See [authentication and tenant mapping](security/authentication.md).
