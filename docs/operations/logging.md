# Structured Logging and Redaction

ThinkPixelAG emits one JSON object per log event through the centralized
`internal/observability/logging` handler. Service code uses `log/slog` and must
receive the configured logger; it must not create alternate production handlers
that bypass correlation or redaction.

## Levels and event shape

`THINKPIXELAG_LOG_LEVEL` or `--log-level` selects `debug`, `info`, `warn`, or
`error`; the default is `info`. Every event contains the standard timestamp,
level, and message fields. Attributes use stable `snake_case` keys and bounded
values. Do not use tenant, principal, run, agent, request, or error strings as
attribute names because dynamic keys defeat indexing and redaction review.

HTTP middleware attaches validated correlation through
`logging.WithCorrelation`. The handler emits non-empty values as root-level
`request_id` and `trace_id`. Record or pre-bound attributes cannot override these
reserved keys, including spelling variants such as `trace-id`. Invalid IDs,
whitespace/control characters, and values longer than 128 bytes are omitted.

## Central redaction

The handler normalizes attribute names case-insensitively and treats hyphens,
dots, and spaces as underscores. It replaces protected values with
`[REDACTED]`, recursively covering `slog.Group`, `slog.LogValuer`, string-keyed
maps, slices/arrays, and exported struct fields. Traversal fails closed at its
depth bound.

Protected fields include:

- authorization/proxy authorization, cookies, and set-cookie values;
- passwords, credentials, secrets, DSNs, and API/client keys;
- access, refresh, ID, bearer, and other token-suffixed fields;
- database and Valkey URLs;
- objectives, prompts, agent inputs, and raw policy input.

Suffix matching also protects application-specific names ending in `_password`,
`_secret`, `_token`, `_api_key`, `_credential`, `_credentials`, or `_dsn`.

Redaction is defense in depth, not permission to collect sensitive content.
Never interpolate secrets, authorization headers, raw objectives/inputs, policy
input, or credentials into the log message or an error string: field-name
redaction cannot reliably discover secrets embedded in prose. Prefer stable
error categories and safe typed attributes. Do not log request/response bodies
or complete header maps by default.

## Correlation contract

- `request_id` identifies one inbound request and is propagated to safe outbound
  calls where the protocol permits it.
- `trace_id` is the active distributed trace identifier once tracing is added in
  ENG-005; until then it may be absent.
- IDs are operational metadata, never identity or authorization evidence.
- Only trusted middleware writes correlation context. Request headers are
  validated before use and never logged as a substitute for the context value.

Tests exercise level filtering, context injection, reserved-key spoofing,
handler immutability, normalized sensitive names, nested containers/structs,
`LogValuer` output, and depth-bounded fail-closed behavior. New sensitive data
classes must extend both the centralized matcher and its table-driven leak tests.
