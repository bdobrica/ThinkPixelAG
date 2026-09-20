# Local administration and harness evaluation

This installs the current source candidate using **real operator bootstrap**,
PostgreSQL, OPA and an optional console. It retains state across restarts. The
included development issuer supplies two separate operators and a caller; it
has no passwords and binds only to loopback. It is not company SSO.

Requirements: Linux (including WSL), Docker, Python 3.13 and the repository's Go
toolchain. Work from the repository root. Use a private Linux home directory for
state, not a shared Windows filesystem. The published rc.1 fixture example under
`deploy/demo` is retained for older installations; do not point this installer
at that database or remove its volumes.

```sh
python3 -m venv .cache/evaluation-venv
.cache/evaluation-venv/bin/pip install --require-hashes -r console/requirements.txt
.cache/evaluation-venv/bin/python deploy/evaluation/local.py up --console
```

Omit `--console` for AG alone. Python belongs to the optional console and this
example's development issuer, never AG's build/runtime. The command builds the
three Go commands; alternatively pass `--bin-dir /path/to/release/binaries/linux-amd64`
(or `linux-arm64`) to use packaged release binaries. It creates a dedicated
persistent Docker database volume, a retained OPA container, private trust/key
files and an approved sample agent using `thinkpixelag-operator bootstrap`.
There is no SQL fixture seeding or reset operation.

Default state: `~/.local/state/thinkpixelag-evaluation`. Set `--state` explicitly
for a second installation, and choose a free five-port range with `--port` on its
first `up`. Saved ports/credentials win on later calls. Defaults:

| Endpoint | Address |
|---|---|
| AG HTTPS and development issuer | `https://127.0.0.1:19455` |
| Optional console | `https://127.0.0.1:19456` |
| Private API, PostgreSQL and OPA | loopback ports 19457, 19458 and 19459 |

Open the console URL. Trust the generated `tls.crt` **only for this local
evaluation**, or use your browser's explicit development-certificate exception.
BFF, AG and the harness helper verify the generated CA normally. Choose
**Operator one** to inspect agents, policies and settings; use **Operator two**
in a separate browser profile for independent approvals. The caller has no
administrative roles. Signing, issuer and TLS keys are separate files.

## Connect the harness and use AG

```sh
install -d -m 700 "$HOME/.local/bin"
install -m 755 integrations/harness/thinkpixelag-harness "$HOME/.local/bin/thinkpixelag-harness"
export PATH="$HOME/.local/bin:$PATH"
export AG_EVAL_STATE="$HOME/.local/state/thinkpixelag-evaluation"
thinkpixelag-harness --config "$AG_EVAL_STATE/harness.json" guidance
thinkpixelag-harness --config "$AG_EVAL_STATE/harness.json" agents
printf '%s\n' 'Summarize incident INC-42' > /tmp/ag-objective.txt
# Replace AGENT_UUID with an ID returned above.
thinkpixelag-harness --config "$AG_EVAL_STATE/harness.json" admit \
  --agent-id AGENT_UUID --objective-file /tmp/ag-objective.txt --idempotency-key incident-42-admit-0001
# Replace RUN_UUID with the admitted Run ID.
thinkpixelag-harness --config "$AG_EVAL_STATE/harness.json" run --run-id RUN_UUID
thinkpixelag-harness --config "$AG_EVAL_STATE/harness.json" cancel \
  --run-id RUN_UUID --idempotency-key incident-42-cancel-0001
```

AG supplies policy/approval/deployment ceilings when caller limits are omitted.
Reuse the same key/body for an uncertain request; new intentions need new keys.
An admitted Run does not launch AR or execute the objective. The sample agent's
image is an explicit metadata placeholder. Follow the
[helper installation guide](../../../integrations/harness/README.md) to expose a
fixed configured wrapper to your harness and append its AGENTS.md snippet once.
Keep tokens outside model state. Refresh the private caller file after 15 minutes:

```sh
.cache/evaluation-venv/bin/python deploy/evaluation/local.py token
```

For trusted operator CLI use, `token --identity operator-one` or `operator-two`
writes a separate private file and prints only its path. Never give these files
to the harness. Use the [API walkthrough](../../operations/administration.md), or
the [console workflow](../../../console/README.md#policy-editing-and-approvals),
for edit → validate → promote → activate → independently approved rollback.
Role expansion uses the same two identities; OPA settings use protected aliases,
not credentials in forms. This minimal installer starts private, tokenless OPA.

## Restart, persistence and recovery

```sh
.cache/evaluation-venv/bin/python deploy/evaluation/local.py status
.cache/evaluation-venv/bin/python deploy/evaluation/local.py restart --console
.cache/evaluation-venv/bin/python deploy/evaluation/local.py console-stop
.cache/evaluation-venv/bin/python deploy/evaluation/local.py backup
```

Restart reopens the same database, keys, bootstrap receipt and managed records.
Console sessions are deliberately lost. Stopping the console leaves AG usable.
Repeat `up --console` to start it again. Keep the state directory and Docker
volume together; back up the database dump plus private signing/issuer keys,
certificates, bootstrap snapshot and installation configuration. Protect backups
as secrets. Certificates last 14 days; renew transport/issuer trust deliberately,
without resetting governance state. For restore, use a separate database and the
[restore procedure](../../operations/backup-recovery.md), retaining the same trust
material; never overwrite a live database merely to retry setup.

Logs are private `api.log`, `identity.log`, `console.log` and `last-command.log`
in the state directory. A failed command retains partial resources for inspection.
A bootstrap mismatch is a review/recovery issue, not permission to force-reset.
Use [protected mapping recovery](../../operations/bootstrap.md#recover-administrator-mappings)
for lockout. No service or volume is automatically removed.

This lightweight example retains transaction-bound audit/outbox data in
PostgreSQL but does not configure an independent evidence receiver. Monitor
outbox growth and configure the [evidence sink](../../configuration.md) for a
longer evaluation. Production signing custody, external IdP qualification,
throughput/p99/HA and AR/gateway execution remain outside this local qualification.
