#!/bin/sh
set -eu

: "${DATABASE_URL:?DATABASE_URL is required}"

BACKUP_DIR="${BACKUP_DIR:-/backups}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
hostname_tag="$(hostname | tr -cd 'A-Za-z0-9._-' | cut -c1-40)"
filename="nexora_${timestamp}_${hostname_tag}.dump"
tmp="${BACKUP_DIR}/.${filename}.tmp"
target="${BACKUP_DIR}/${filename}"

mkdir -p "$BACKUP_DIR"
umask 077

cleanup() {
  rm -f "$tmp"
}
trap cleanup EXIT INT TERM

pg_dump   --dbname="$DATABASE_URL"   --format=custom   --compress=6   --no-owner   --no-acl   --file="$tmp"

pg_restore --list "$tmp" >/dev/null
mv "$tmp" "$target"
sha256sum "$target" > "${target}.sha256"

find "$BACKUP_DIR" -type f \( -name 'nexora_*.dump' -o -name 'nexora_*.dump.sha256' \)   -mtime "+$BACKUP_RETENTION_DAYS" -delete

trap - EXIT INT TERM
printf '%s\n' "$target"
