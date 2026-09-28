#!/bin/sh
set -eu

fail() {
  printf '%s\n' "production configuration invalid: $1" >&2
  exit 1
}

require_value() {
  name="$1"
  eval "value=\${$name:-}"
  [ -n "$value" ] || fail "$name is required"
  case "$value" in
    CHANGE_ME*|*CHANGE_ME*|local-dev-*|change-me-*)
      fail "$name still contains a development placeholder"
      ;;
  esac
}

require_false() {
  name="$1"
  eval "value=\${$name:-}"
  [ "$value" = "false" ] || fail "$name must be false in production"
}

require_https() {
  name="$1"
  eval "value=\${$name:-}"
  case "$value" in
    https://*) ;;
    *) fail "$name must use https://" ;;
  esac
}

require_min_length() {
  name="$1"
  minimum="$2"
  eval "value=\${$name:-}"
  [ "${#value}" -ge "$minimum" ] || fail "$name must be at least $minimum characters"
}

[ "${ENVIRONMENT:-}" = "production" ] || fail "ENVIRONMENT must be production"

for name in   OPENROUTER_API_KEY   POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD DATABASE_URL   REDIS_PASSWORD REDIS_URL CELERY_BROKER_URL CELERY_RESULT_BACKEND   API_KEY_HASH_PEPPER SESSION_SECRET CONTROL_ADMIN_TOKEN PLAYGROUND_INTERNAL_TOKEN   SMTP_HOST SMTP_FROM_EMAIL
do
  require_value "$name"
done

require_https PUBLIC_BASE_URL
require_https DASHBOARD_URL
require_min_length API_KEY_HASH_PEPPER 32
require_min_length SESSION_SECRET 32
require_min_length CONTROL_ADMIN_TOKEN 24
require_min_length PLAYGROUND_INTERNAL_TOKEN 32
require_min_length POSTGRES_PASSWORD 16
require_min_length REDIS_PASSWORD 16

require_false LOCAL_BOOTSTRAP_ENABLED
require_false DEV_EXPOSE_PASSWORD_RESET_TOKEN
require_false DEV_EXPOSE_EMAIL_VERIFICATION_TOKEN

case "${DATABASE_URL:-}" in
  *"@postgres:"*) ;;
  *) fail "DATABASE_URL must target the production Compose postgres service" ;;
esac

case "${REDIS_URL:-}" in
  *"@redis:"*) ;;
  *) fail "REDIS_URL must target the production Compose redis service" ;;
esac

printf '%s\n' "production configuration validation passed"
