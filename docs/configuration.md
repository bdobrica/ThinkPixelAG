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
| `THINKPIXELAG_HTTP_READ_HEADER_TIMEOUT` | `--http-read-header-timeout` | `5s` | greater than zero, at most `5m` |
| `THINKPIXELAG_HTTP_READ_TIMEOUT` | `--http-read-timeout` | `15s` | greater than zero, at most `5m` |
| `THINKPIXELAG_HTTP_WRITE_TIMEOUT` | `--http-write-timeout` | `30s` | greater than zero, at most `5m` |
| `THINKPIXELAG_HTTP_IDLE_TIMEOUT` | `--http-idle-timeout` | `1m` | greater than zero, at most `5m` |
| `THINKPIXELAG_HTTP_SHUTDOWN_TIMEOUT` | `--http-shutdown-timeout` | `20s` | greater than zero, at most `5m` |
| `THINKPIXELAG_DATABASE_URL` | none | none | required; `postgres://` or `postgresql://` |
| `THINKPIXELAG_DATABASE_CONNECT_TIMEOUT` | `--database-connect-timeout` | `5s` | greater than zero, at most `5m` |
| `THINKPIXELAG_OPA_URL` | `--opa-url` | `http://127.0.0.1:8181` | absolute HTTP(S), no credentials/query/fragment; production requires HTTPS except loopback |
| `THINKPIXELAG_OPA_DECISION_PATH` | `--opa-decision-path` | `/v1/data/thinkpixelag/decision` | absolute path without query/fragment |
| `THINKPIXELAG_OPA_TIMEOUT` | `--opa-timeout` | `2s` | greater than zero, at most `5m` |
| `THINKPIXELAG_OPA_BEARER_TOKEN` | none | unset | optional secret |
| `THINKPIXELAG_VALKEY_URL` | none | unset | optional `redis://` or `rediss://`; production requires TLS except loopback |
| `THINKPIXELAG_VALKEY_TIMEOUT` | `--valkey-timeout` | `500ms` | greater than zero, at most `5m` |
| `THINKPIXELAG_OIDC_ISSUER_URL` | `--oidc-issuer-url` | none | required absolute HTTP(S), no credentials/query/fragment; HTTPS except loopback |
| `THINKPIXELAG_OIDC_AUDIENCE` | `--oidc-audience` | none | required, no surrounding whitespace or control characters |

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
export THINKPIXELAG_DATABASE_URL='postgresql://thinkpixelag:local-only@127.0.0.1:5432/thinkpixelag?sslmode=disable'
export THINKPIXELAG_OIDC_ISSUER_URL='http://127.0.0.1:5556/dex'
export THINKPIXELAG_OIDC_AUDIENCE='thinkpixelag-local'
```

These example credentials are for isolated local development only. ENG-009 will
provide the pinned local dependencies and test credentials. Production identity,
database, OPA, and optional Valkey configuration must use managed secret delivery
and encrypted transport as described above.
