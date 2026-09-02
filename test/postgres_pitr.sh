#!/usr/bin/env bash
set -euo pipefail

docker_bin="${DOCKER:-docker}"
postgres_image="${POSTGRES_IMAGE:?POSTGRES_IMAGE is required}"
work_dir="$(mktemp -d /tmp/thinkpixelag-pitr.XXXXXX)"
run_id="$(basename "$work_dir")"
primary="${run_id}-primary"
restore="${run_id}-restore"
password="pitr-local-only"
started_at="$(date +%s)"
max_rto=0

cleanup() {
  "$docker_bin" rm -f "$primary" "$restore" >/dev/null 2>&1 || true
  "$docker_bin" run --rm -v "$work_dir:/qualification-cleanup" "$postgres_image" sh -c 'rm -rf /qualification-cleanup/*' >/dev/null 2>&1 || true
  rmdir "$work_dir" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

mkdir -p "$work_dir/primary" "$work_dir/archive" "$work_dir/backup" "$work_dir/prior-migrations"
chmod 0777 "$work_dir/primary" "$work_dir/archive" "$work_dir/backup"
cp migrations/0{01,02,03,04,05,06,07,08,09}_*.sql "$work_dir/prior-migrations/"
cp migrations/0{10,11,12,13,14,15,16,17}_*.sql "$work_dir/prior-migrations/"

"$docker_bin" run -d --name "$primary" \
  -e POSTGRES_DB=thinkpixelag -e POSTGRES_USER=thinkpixelag -e POSTGRES_PASSWORD="$password" \
  -e PGDATA=/var/lib/postgresql/data \
  -v "$work_dir/primary:/var/lib/postgresql/data" -v "$work_dir/archive:/archive" -v "$work_dir/backup:/backup" \
  -v "$PWD/.cache/bin/thinkpixelag-migrate:/usr/local/bin/thinkpixelag-migrate:ro" \
  -v "$PWD/migrations:/all-migrations:ro" -v "$work_dir/prior-migrations:/migrations:ro" -v "$PWD/test:/qualification:ro" \
  "$postgres_image" -c wal_level=replica -c archive_mode=on -c 'archive_command=test ! -f /archive/%f && cp %p /archive/%f' >/dev/null

for _ in $(seq 1 60); do
  if "$docker_bin" exec "$primary" pg_isready -U thinkpixelag -d thinkpixelag >/dev/null 2>&1; then break; fi
  sleep 1
done
"$docker_bin" exec -e THINKPIXELAG_DATABASE_URL="postgresql://thinkpixelag:$password@127.0.0.1:5432/thinkpixelag?sslmode=disable" "$primary" thinkpixelag-migrate >/dev/null
"$docker_bin" exec "$primary" psql -U thinkpixelag -d thinkpixelag -f /qualification/postgres_pitr_fixture.sql >/dev/null
"$docker_bin" exec "$primary" pg_basebackup -U thinkpixelag -D /backup/base -Fp -X none -c fast
"$docker_bin" exec -u 0 "$primary" chmod -R a+rwX /backup/base
"$docker_bin" exec "$primary" psql -U thinkpixelag -d thinkpixelag -Atqc "SELECT pg_create_restore_point('ops009_before_governance')" >/dev/null
"$docker_bin" exec "$primary" psql -U thinkpixelag -d thinkpixelag -f /qualification/postgres_pitr_governance.sql >/dev/null
"$docker_bin" exec "$primary" psql -U thinkpixelag -d thinkpixelag -Atqc "SELECT pg_create_restore_point('ops009_after_governance')" >/dev/null
wal_segment="$("$docker_bin" exec "$primary" psql -U thinkpixelag -d thinkpixelag -Atqc 'SELECT pg_walfile_name(pg_switch_wal())' | tr -d '\r')"
for _ in $(seq 1 60); do
  [[ -f "$work_dir/archive/$wal_segment" ]] && break
  sleep 1
done
[[ -f "$work_dir/archive/$wal_segment" ]] || { printf 'PITR WAL segment was not archived\n' >&2; exit 1; }
"$docker_bin" stop -t 30 "$primary" >/dev/null

encryption_key="$work_dir/backup.key"
encrypted_backup="$work_dir/base-backup.tar.enc"
openssl rand -out "$encryption_key" 32
tar -C "$work_dir/backup" -cf - base | openssl enc -aes-256-cbc -pbkdf2 -salt -pass file:"$encryption_key" -out "$encrypted_backup"
rm -rf "$work_dir/backup/base"

restore_target() {
  local target="$1" expected_epoch="$2" expected_policy_epoch="$3" expected_tenant_revocation_epoch="$4"
  local expected_agent_revocation_epoch="$5" expected_outbox="$6" expected_audit="$7" expected_consumed="$8"
  local restore_dir="$work_dir/restore-$target"
  local restore_started_at
  restore_started_at="$(date +%s)"
  mkdir -p "$restore_dir"
  chmod 0777 "$restore_dir"
  openssl enc -d -aes-256-cbc -pbkdf2 -pass file:"$encryption_key" -in "$encrypted_backup" | tar -C "$work_dir/backup" -xf -
  cp -a "$work_dir/backup/base/." "$restore_dir/"
  rm -rf "$work_dir/backup/base"
  printf "restore_command = 'cp /archive/%%f %%p'\nrecovery_target_name = '%s'\nrecovery_target_action = 'promote'\n" "$target" >> "$restore_dir/postgresql.auto.conf"
  touch "$restore_dir/recovery.signal"
  "$docker_bin" run --rm -v "$restore_dir:/restore" "$postgres_image" sh -c 'chown -R postgres:postgres /restore && chmod 0700 /restore'
  "$docker_bin" run -d --name "$restore" -e PGDATA=/var/lib/postgresql/data \
    -v "$restore_dir:/var/lib/postgresql/data" -v "$work_dir/archive:/archive:ro" \
    -v "$PWD/.cache/bin/thinkpixelag-migrate:/usr/local/bin/thinkpixelag-migrate:ro" \
    -v "$PWD/migrations:/migrations:ro" -v "$PWD/scripts:/qualification-scripts:ro" -v "$PWD/test:/qualification:ro" \
    "$postgres_image" >/dev/null
  for _ in $(seq 1 60); do
    if "$docker_bin" exec "$restore" pg_isready -U thinkpixelag -d thinkpixelag >/dev/null 2>&1; then break; fi
    sleep 1
  done
  "$docker_bin" exec "$restore" psql -U thinkpixelag -d thinkpixelag \
    -v expected_security_epoch="$expected_epoch" -v expected_policy_epoch="$expected_policy_epoch" \
    -v expected_tenant_revocation_epoch="$expected_tenant_revocation_epoch" -v expected_agent_revocation_epoch="$expected_agent_revocation_epoch" \
    -v expected_outbox="$expected_outbox" -v expected_audit="$expected_audit" -v expected_consumed="$expected_consumed" \
    -f /qualification/postgres_pitr_assert.sql >/dev/null
  "$docker_bin" exec -e THINKPIXELAG_DATABASE_URL="postgresql://thinkpixelag:$password@127.0.0.1:5432/thinkpixelag?sslmode=disable" "$restore" thinkpixelag-migrate >/dev/null
  "$docker_bin" exec "$restore" psql -U thinkpixelag -d thinkpixelag -f /qualification-scripts/check-restored-invariants.sql >/dev/null
  version="$("$docker_bin" exec "$restore" psql -U thinkpixelag -d thinkpixelag -Atqc 'SELECT version FROM thinkpixelag_schema_version' | tr -d '\r')"
  [[ "$version" == "18" ]] || { printf 'forward migration ended at schema version %s\n' "$version" >&2; exit 1; }
  "$docker_bin" rm -f "$restore" >/dev/null
  rto="$(( $(date +%s) - restore_started_at ))"
  if (( rto > max_rto )); then max_rto="$rto"; fi
  printf 'postgres-pitr: target=%s rto_seconds=%s invariant_check=passed\n' "$target" "$rto"
}

restore_target ops009_before_governance 0 4 5 6 1 1 0
restore_target ops009_after_governance 1 5 6 7 2 2 10

elapsed="$(( $(date +%s) - started_at ))"
backup_digest="$(sha256sum "$encrypted_backup" | awk '{print $1}')"
printf 'postgres-pitr: encrypted physical backup and WAL restore passed image=%s backup_sha256=%s targets=2 schema=17->18 observed_rto_seconds=%s observed_rpo_transactions=0 elapsed_seconds=%s\n' "$postgres_image" "$backup_digest" "$max_rto" "$elapsed"
