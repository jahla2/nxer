# Production Runtime and Release Validation

This runbook covers the production-oriented Docker stack and the release evidence required before external users are invited.

## Production stack

The production Compose file is `infra/docker-compose.prod.yml`. It differs from the local development stack in several important ways:

- Go is compiled into a stripped binary before runtime.
- FastAPI and Celery dependencies are installed during image build, not container startup.
- React is compiled once and served as static assets.
- Application containers run as non-root users.
- Application filesystems are read-only except for explicit temporary filesystems.
- PostgreSQL and Redis are not published to the host.
- The edge proxy binds to `127.0.0.1:8080` by default.
- Migrations are packaged into an immutable release-tools image.
- Production configuration is validated before migrations or application startup.
- The Nginx edge adds CSP, HSTS, framing, MIME-sniffing, referrer, and browser-permission headers.
- Redis uses `noeviction`; exhausted memory fails writes instead of silently evicting coordination keys.

## First deployment

Create the production environment file:

```bash
cp .env.production.example .env.production
```

Replace every `CHANGE_ME` value. Production startup rejects:

- development placeholders,
- weak server secrets,
- insecure dashboard URLs,
- local bootstrap mode,
- development reset/verification token exposure,
- missing SMTP delivery settings.

Build and start:

```bash
docker compose -f infra/docker-compose.prod.yml up -d --build
```

Verify:

```bash
curl -fsS http://127.0.0.1:8080/edge-health
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:8080/ready
curl -fsS http://127.0.0.1:8080/api/health
curl -fsS http://127.0.0.1:8080/api/ready
```

The production edge should remain loopback-only when Cloudflare Tunnel is the public ingress.

## Cloudflare Tunnel

The optional `tunnel` profile runs `cloudflared`:

```bash
docker compose -f infra/docker-compose.prod.yml --profile tunnel up -d
```

Set `CLOUDFLARE_TUNNEL_TOKEN` in `.env.production` and configure the named tunnel public hostname in Cloudflare to use:

```text
http://nginx:8080
```

Keep the host port bound to loopback unless there is a separate firewall/reverse-proxy requirement.

## Database backup

Create a custom-format PostgreSQL backup with checksum verification:

```bash
docker compose -f infra/docker-compose.prod.yml --profile ops run --rm backup
```

Backups are written to the `postgres_backups` named volume. Each dump has a matching SHA-256 file. Old backups are deleted according to `BACKUP_RETENTION_DAYS` (14 days by default).

A successful backup command verifies the dump catalog with `pg_restore --list` before publishing the file.

## Restore drill

Restore is intentionally guarded. The target database must already exist.

Example:

```bash
BACKUP_FILE=/backups/nexora_YYYYMMDDTHHMMSSZ_HOST.dump \
RESTORE_DATABASE_URL='postgresql://nexora:...@postgres:5432/nexora_restore' \
ALLOW_DESTRUCTIVE_RESTORE=yes \
docker compose -f infra/docker-compose.prod.yml --profile restore run --rm restore
```

The restore command:

1. verifies the checksum when present,
2. validates the PostgreSQL dump catalog,
3. requires explicit destructive-restore approval,
4. uses `--clean --if-exists`,
5. stops on the first restore error.

Never test a restore for the first time against the live production database. Restore into a clean database and verify application-critical rows and migrations first.

## CI release evidence

The CI pipeline validates the production runtime by:

- parsing the production Compose file,
- building all production images,
- starting the production stack with non-secret test credentials,
- waiting for the hardened edge,
- checking gateway/control health and readiness,
- checking SPA deep links,
- checking required security headers,
- checking application images run as non-root,
- checking production application containers do not use source-code bind mounts,
- creating a real PostgreSQL custom-format backup,
- restoring it into a new database,
- verifying both schema migrations and seeded probe data.

These checks validate the runtime shape without using a real provider key or real SMTP credentials.

## Remaining release-validation suites

Production runtime validation is only one part of Phase 7. Before inviting external users, also complete:

- OpenAI Python/JavaScript client contract tests,
- mocked provider and SSE contract tests,
- 50–100 concurrent mocked streaming clients,
- burst-rate, concurrency, and idempotency load tests,
- disconnect/cancellation recovery,
- explicit Redis/PostgreSQL restart and outage tests,
- security regression automation,
- real Cloudflare public-hostname validation,
- real provider catalog/inference validation using deployment credentials.

Do not treat CI smoke tests as proof of real external-provider or Cloudflare connectivity.
