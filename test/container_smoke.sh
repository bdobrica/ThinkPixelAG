#!/usr/bin/env bash
set -euo pipefail

docker_bin="${DOCKER:-docker}"
image="${IMAGE:-thinkpixelag:dev}"
container="thinkpixelag-smoke-$$"
expected_version="${VERSION:-smoke-version}"
expected_revision="${REVISION:-smoke-revision}"

cleanup() {
    "$docker_bin" rm --force "$container" >/dev/null 2>&1 || true
}
trap cleanup EXIT

configured_user=$("$docker_bin" image inspect --format '{{.Config.User}}' "$image")
test "$configured_user" = "65532:65532"
actual_version=$("$docker_bin" image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$image")
actual_revision=$("$docker_bin" image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")
test "$actual_version" = "$expected_version"
test "$actual_revision" = "$expected_revision"
if "$docker_bin" run --rm --entrypoint /bin/sh "$image" -c true >/dev/null 2>&1; then
    printf 'container-smoke: runtime unexpectedly contains /bin/sh\n' >&2
    exit 1
fi

"$docker_bin" run --detach --name "$container" \
    --read-only \
    --cap-drop ALL \
    --security-opt no-new-privileges \
    --tmpfs /tmp:rw,noexec,nosuid,size=1m \
    --publish 127.0.0.1::8080 \
    --env THINKPIXELAG_DATABASE_URL=postgresql://smoke:smoke@127.0.0.1:5432/smoke?sslmode=disable \
    --env THINKPIXELAG_OIDC_ISSUER_URL=http://127.0.0.1:5556/smoke \
    --env THINKPIXELAG_OIDC_AUDIENCE=thinkpixelag-smoke \
    "$image" >/dev/null

address=$("$docker_bin" port "$container" 8080/tcp)
for _ in {1..50}; do
    if curl --fail --silent --show-error "http://$address/livez" >/dev/null; then
        break
    fi
    running=$("$docker_bin" inspect --format '{{.State.Running}}' "$container")
    if test "$running" != "true"; then
        "$docker_bin" logs "$container" >&2
        exit 1
    fi
    sleep 0.1
done
curl --fail --silent --show-error "http://$address/livez" >/dev/null
readiness_status=$(curl --silent --output /dev/null --write-out '%{http_code}' "http://$address/readyz")
test "$readiness_status" = "503"

running_uid=$("$docker_bin" top "$container" | awk 'NR == 2 {print $1}')
test "$running_uid" = "65532"
read_only=$("$docker_bin" inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$container")
test "$read_only" = "true"

"$docker_bin" stop --time 5 "$container" >/dev/null
exit_code=$("$docker_bin" inspect --format '{{.State.ExitCode}}' "$container")
test "$exit_code" = "0"
printf 'container-smoke: shell-free non-root image, read-only runtime, liveness/fail-closed readiness, metadata, and SIGTERM verified\n'
