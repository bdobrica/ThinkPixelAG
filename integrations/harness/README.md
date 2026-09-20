# Install the harness helper

This Python 3.11+ standard-library helper connects an existing harness to a
`0.1.0-rc.2` AG with the [v1 guidance API](../../docs/contracts/harness-guidance.md).
Follow the [local installation](../../docs/examples/local-sandbox/README.md) or
[operator bootstrap](../../docs/operations/bootstrap.md) to provision an approved
agent first. The older rc.1 image predates this API.

## Trusted host setup

Run these steps as the operator on the Linux/macOS integration host, outside
model instructions. Expose AG through HTTPS with a certificate trusted by this
host; plain HTTP, redirects and ambient HTTP proxies are deliberately rejected.
A local TLS reverse proxy with a locally trusted CA is sufficient for development.

From the repository root:

```sh
install -d -m 700 "$HOME/.local/bin" "$HOME/.config/thinkpixelag" "$HOME/.local/state/thinkpixelag-harness"
install -m 755 integrations/harness/thinkpixelag-harness "$HOME/.local/bin/thinkpixelag-harness"
export PATH="$HOME/.local/bin:$PATH"
```

Have the deployment's OIDC login/credential agent write a short-lived **caller**
bearer token to `~/.config/thinkpixelag/caller.token` (mode `0600`). Do not paste
it into a prompt, command argument, AGENTS.md or repository. Login and token
renewal belong to that host adapter; this helper does not implement an IdP login.
The token must have the issuer/audience and tenant mapping configured by AG.

Create `~/.config/thinkpixelag/harness.json` with absolute paths, substituting
your user home and the exact AG origin:

```json
{
  "origin": "https://ag.example.test",
  "token_file": "/home/alice/.config/thinkpixelag/caller.token",
  "state_dir": "/home/alice/.local/state/thinkpixelag-harness"
}
```

```sh
chmod 600 "$HOME/.config/thinkpixelag/harness.json" "$HOME/.config/thinkpixelag/caller.token"
thinkpixelag-harness guidance
```

For a private CA add `"ca_file": "/absolute/path/to/ca.pem"`; verification stays
enabled. Origin has no path, query or trailing slash. Config/token files must be
owned private regular files, not symlinks. Keep parent directories protected.
The cache contains scoped guidance, not tokens, and uses private files.

Expose the installed command through your harness's approved tool/command
mechanism. Prevent that harness from reading credential storage or modifying
helper configuration using host permissions/sandbox policy. A same-user shell
with unrestricted file access is **not** credential isolation: the manual local
walkthrough establishes behavior, not hardened custody. The helper never prints
tokens, remote error bodies or configuration. Provider and signing keys do not
belong on this interface.

## Append instructions, then use AG

Review [AGENTS.snippet.md](AGENTS.snippet.md) and append its section **once** to
the chosen project's existing AGENTS.md. Preserve all existing instructions.
For example, from the repository root, with an existing project path:

```sh
cat integrations/harness/AGENTS.snippet.md >> /path/to/project/AGENTS.md
```

Start the harness in that project. Its first platform command is
`thinkpixelag-harness guidance`. A minimal governance interaction is:

```sh
thinkpixelag-harness agents
# Select an approved agent ID from that response; write the requested objective.
printf '%s\n' 'Summarize incident INC-42' > objective.txt
thinkpixelag-harness admit --agent-id AGENT_UUID --objective-file objective.txt --idempotency-key incident-42-admit-0001
# Use the returned Run ID below.
thinkpixelag-harness guidance --run-id RUN_UUID
thinkpixelag-harness run --run-id RUN_UUID
thinkpixelag-harness cancel --run-id RUN_UUID --idempotency-key incident-42-cancel-0001
```

Replace `AGENT_UUID`/`RUN_UUID` with actual IDs. Admission sends the objective;
AG resolves authoritative policy/approval/deployment limits. Keep an uncertain
mutation's original key and body for retries. Use a new key for a new intent.
Do not automatically retry conflicts as new mutations. List output deliberately
omits free-form descriptions; use the authorized API for richer registry views.

Every command revalidates guidance online before its operation. Admission and
cancellation refresh the resulting Run context. A successful mutation whose
follow-up refresh fails still returns its Run ID with `guidance_refreshed:false`;
stop further platform actions until `guidance --run-id RUN_UUID` succeeds.
Switching Runs or caller tokens selects a separate cache entry. Guidance expires
within 30 seconds; conditional responses never extend it. A discovery conflict
causes one fresh fetch; an operation conflict refreshes guidance but never retries
the mutation. Repeated conflict, denied access, failed TLS, unavailable AG or
expired guidance stops dependent operations, with a nonzero exit status.

`guidance --json` exposes the structured contract for another trusted adapter.
A capability being listed is not permission: AG authorizes each actual call.
An admitted Run does not launch execution. This helper currently supports
admission/read/cancel; AR-backed execution, completion and gateway accounting
remain a separate integration task.

See the [recorded walkthrough](../../docs/evidence/results/harness-guidance.json)
and [integration boundaries](../../docs/operations/integrations.md).
