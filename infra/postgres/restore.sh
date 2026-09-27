#!/bin/sh
set -eu

: "${RESTORE_DATABASE_URL:?RESTORE_DATABASE_URL is required}"
: "${BACKUP_FILE:?BACKUP_FILE is required}"

[ "${ALLOW_DESTRUCTIVE_RESTORE:-}" = "yes" ] || {
  printf '%s\n' "restore refused: set ALLOW_DESTRUCTIVE_RESTORE=yes" >&2
  exit 2
}

[ -f "$BACKUP_FILE" ] || {
  printf '%s\n' "restore refused: backup file does not exist" >&2
  exit 2
}

if [ -f "${BACKUP_FILE}.sha256" ]; then
  (
    cd "$(dirname "$BACKUP_FILE")"
    sha256sum -c "$(basename "$BACKUP_FILE").sha256"
  )
fi

pg_restore --list "$BACKUP_FILE" >/dev/null

pg_restore   --dbname="$RESTORE_DATABASE_URL"   --clean   --if-exists   --no-owner   --no-acl   --exit-on-error   "$BACKUP_FILE"

printf '%s\n' "restore completed"
