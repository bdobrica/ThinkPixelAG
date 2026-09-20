# Retained-cluster lifecycle rehearsal

`make test-retained-lifecycle` extends the disposable Kind test with governed
HTTP workflows, observed HPA behavior and application digest upgrade/rollback.
It uses the isolated [load fixture](load-testing.md) and the current database
writer recorded by [resilience qualification](resilience-testing.md). It never
uninstalls the namespace, deletes PVCs, or restarts the fenced former writer.

Run `make test-lifecycle` for local regressions covering immutable image inputs,
rollback after a candidate failure, preservation of both images' state and
refusal to retry an uncertain admission. Python 3 is the only harness runtime;
there are no additional Python dependencies. The full repository gate remains
`make verify`.

## Preconditions

Use SSH access with `sudo kubectl`, the governed API fixture, valid private
caller/foreign identities and synthetic approved agent state. Keep generated
identities and checkpoint directories outside version control. Select the
**current** database writer explicitly with `--primary`. Run scenarios serially
without another test runner, writer or operator changing the fixture.

Admission intent is saved privately before the first mutation. An existing
checkpoint stops that workflow before another request is sent. If admission
fails or times out, inspect the private intent and authoritative database before
another attempt. Do not delete the checkpoint to bypass the known admission
completion-window failure. Reports omit raw bodies, credentials and tenant/Run
identifiers. Checkpoints contain synthetic request/response state and remain
private with mode 0600.

```sh
make test-retained-lifecycle LIFECYCLE_ARGS='--ssh-host operator@controller --namespace isolated-ops --base https://test-api.example --identities /private/identities --state-dir /private/ops012 --report /private/posture.json --primary current-writer --scenario posture --allow-fault-injection'
```

The `posture` scenario checks running Pods for UID 65532, non-root execution,
RuntimeDefault seccomp, read-only roots, no privilege escalation, all capabilities
dropped and no service-account token mount. It also checks schema 18, readiness
and metrics. These checks inspect the live Pod configuration and service; they
do not execute arbitrary commands inside the shell-less production image.

## Core workflow and application upgrade

`--scenario workflow` exercises a synthetic approved agent through the deployed
OIDC/OPA/PostgreSQL composition: admission, exact replay, conflicting key rejection,
own-tenant read, foreign-tenant hiding, invalid-token denial, durable signal and
replay, cancellation and replay, ordered event streaming and cursor resume.
Exactly three Run events must exist afterward. Restored-state database invariants
must hold. Writes are serial with pauses; this is correctness evidence, not
throughput qualification. Registration/policy setup uses the existing fixture;
this does not claim a new registry administration, AR execution or settlement
workflow.

For `--scenario upgrade`, additionally supply
`--candidate registry.example/ag@sha256:<64-hex-digest>`. Build and publish a
compatible ARM64 image first. Mutable tags and the currently deployed digest
are rejected. The runner:

1. Records the original digest and runs the baseline workflow.
2. Applies a retained digest-named migration Job using the existing migration
   Secret and restricted Pod template, then checks schema compatibility.
3. Rolls out the candidate digest, checks ready Pod image convergence, verifies
   prior admission replay/terminal state, and runs a new workflow.
4. Restores the original digest in `finally`, even if candidate checks fail.
5. Checks the durable state created by both images and runs another workflow
   after rollback, then repeats the live posture checks.

This rehearsal supports the current schema-18 pair. It is not an instruction to
roll arbitrary releases back across incompatible migrations. Database rollback
is never attempted. The earlier Kind test covers fresh install and uninstall;
that script tears its cluster down and must not be redirected to a retained
cluster.

## Observed autoscaling on small hardware

`--scenario hpa` requires metrics-server and three available API replicas. It
refuses to add a second HPA when another named controller already owns the
Deployment. It retains `ops012-api` with a three-to-four-replica bound.

For this diagnostic only, it measures API-container CPU against a 20m average
value and uses a 60-second downscale stabilization window. Sixteen clients make
at most 40 protected reads per second. The test requires observed scale-out to
four replicas, zero read errors, and scale-in to three after traffic stops,
without changing the target during that scale-in observation. It then retains
a 175m API-container target and the normal 300-second downscale stabilization
window, including on failure. API resource requests/limits are unchanged.

This proves controller response to actual measured CPU on this hardware. It
**does not qualify the production 70% whole-Pod CPU policy or its 3–12 replica
capacity**. The production HPA manifest and SLOs remain unchanged. Container CPU
metrics intentionally isolate application demand from the OPA sidecar; see the
[Kubernetes HPA documentation](https://kubernetes.io/docs/concepts/workloads/autoscaling/horizontal-pod-autoscale/).

See [OPS-012 evidence](../../evidence/README.md#recovery) for exact outcomes and image provenance.
