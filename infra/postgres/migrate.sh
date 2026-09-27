#!/bin/sh
set -eu

: "${DATABASE_URL:?DATABASE_URL is required}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-/migrations}"

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
CREATE TABLE IF NOT EXISTS schema_migrations (
    version text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
SQL

for file in "$MIGRATIONS_DIR"/[0-9][0-9][0-9]_*.sql; do
    [ -f "$file" ] || continue
    version="$(basename "$file" .sql)"
    applied="$(psql "$DATABASE_URL" -Atqc "SELECT 1 FROM schema_migrations WHERE version = '$version'")"

    if [ "$applied" = "1" ]; then
        echo "Skipping already-applied migration $version"
        continue
    fi

    echo "Applying migration $version"
    tmp="$(mktemp)"
    {
        echo "BEGIN;"
        cat "$file"
        printf "\nINSERT INTO schema_migrations(version) VALUES ('%s');\n" "$version"
        echo "COMMIT;"
    } > "$tmp"

    if ! psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$tmp"; then
        rm -f "$tmp"
        exit 1
    fi
    rm -f "$tmp"
done
