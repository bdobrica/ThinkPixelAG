#!/bin/sh
set -eu

source_database="$POSTGRES_DB"
restore_database="thinkpixelag_restore_$$"
dump_file="/tmp/$restore_database.dump"
cleanup() {
  dropdb --if-exists --force -U "$POSTGRES_USER" "$restore_database" >/dev/null 2>&1 || true
  rm -f "$dump_file"
  rm -f /tmp/thinkpixelag-check-restored-invariants.sql
}
trap cleanup EXIT INT TERM

pg_dump -U "$POSTGRES_USER" -d "$source_database" --format=custom --no-owner --no-privileges --file="$dump_file"
createdb -U "$POSTGRES_USER" "$restore_database"
pg_restore -U "$POSTGRES_USER" -d "$restore_database" --no-owner --no-privileges --exit-on-error "$dump_file"
psql -U "$POSTGRES_USER" -d "$restore_database" -v ON_ERROR_STOP=1 -f /tmp/thinkpixelag-check-restored-invariants.sql
printf 'backup-restore: logical backup restored and authoritative invariants passed database=%s\n' "$restore_database"
