# Quick start: install AG and connect a harness

Use the [local installation](examples/local-sandbox/README.md) to start persistent
PostgreSQL, OPA, AG and an optional console through the real operator bootstrap:

```sh
python3 -m venv .cache/evaluation-venv
.cache/evaluation-venv/bin/pip install --require-hashes -r console/requirements.txt
.cache/evaluation-venv/bin/python deploy/evaluation/local.py up --console
```

Run from this checkout on Linux/WSL with Docker, Python 3.13 and Go installed.
The command prints the AG/console URLs and private state path. It creates two
local development operators and a caller. Omit `--console` to use AG alone.
The local guide explains certificate trust and using packaged Go binaries.

Open the console and sign in as Operator one. Inspect the approved agent, create
and validate a policy draft, or review role mappings and OPA readiness. Use a
separate browser profile as Operator two for independent approvals.

Then [install the harness helper](../integrations/harness/README.md) and point it
at the generated private `harness.json`. The
[local guide's commands](examples/local-sandbox/README.md#connect-the-harness-and-use-ag)
walk through discovery, objective-only admission, read and cancellation. AG
resolves authoritative limits internally; optional caller limits can only narrow
them. An `ADMITTED` Run is not proof that AR executed the objective.

State and keys are retained. The local issuer and software signing are evaluation
facilities, not production SSO/key custody. For a company PoC with existing
identity and dependencies, use the [Kubernetes example](examples/team-poc/README.md).
[Configuration](configuration.md) and [operations](operations/README.md) cover
longer-lived deployments, monitoring, backups and recovery.
