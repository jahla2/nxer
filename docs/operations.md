# Nexora Operations and Background Jobs

This document describes the V0.1.0-alpha operational ownership introduced in Phase 5.

## Catalog ownership

Celery owns provider catalog discovery. The worker:

1. Fetches the provider model catalog with bounded timeout/retry behavior.
2. Keeps only zero-cost text-capable models.
3. Reconciles those models into PostgreSQL while preserving existing model UUIDs.
4. Marks disappeared provider models inactive in the same database transaction.
5. Updates `catalog_sync_state`.
6. Caches provider-neutral public catalog metadata in Redis.

The Go gateway does **not** poll the provider catalog. It reloads active routing records from PostgreSQL every `CATALOG_RELOAD_INTERVAL_SECONDS` (15 seconds by default). This keeps provider discovery outside the request data plane and lets catalog changes reach inference without restarting the gateway.

`auto-free` is seeded by migration, so a clean database always has the virtual free-router route before the first Celery sync finishes.

## Celery schedules

| Schedule | Task | Default |
| --- | --- | --- |
| Free-model catalog | `nexora.sync_model_catalog` | every 10 minutes |
| Usage aggregation | `nexora.aggregate_usage` | every 60 seconds |
| Housekeeping | `nexora.cleanup_housekeeping` | every 60 minutes |

A worker-ready signal also queues an immediate catalog synchronization after worker startup.

Tasks use late acknowledgements, reject-on-worker-loss, bounded execution time and low prefetch. Catalog synchronization and usage aggregation have bounded retries. All tasks are idempotent or transactionally safe for redelivery.

## Usage aggregation

Raw gateway usage is written to `usage_events`. Each event has a nullable `aggregated_at` marker.

The aggregator selects a bounded batch of unprocessed rows with `FOR UPDATE SKIP LOCKED`, increments the matching `usage_daily` dimensions, and marks those same events aggregated in one PostgreSQL transaction. If the transaction fails, neither the daily totals nor the marker commits. A retry therefore does not double count completed batches.

Migration 009 establishes a one-time exact baseline from existing raw events before enabling the marker-based incremental path.

## Retention and housekeeping

Housekeeping deletes in bounded batches instead of one unbounded transaction.

Default retention:

- request metadata: 30 days
- audit logs: 90 days
- background job history: 14 days
- expired password/email-verification tokens: 7 days
- expired idempotency records: removed after expiry

Raw usage events are deleted only after `aggregated_at` is set, so retention cannot discard data that has not reached `usage_daily`.

Running background jobs older than `JOB_RUN_STALE_MINUTES` are marked failed so dead workers are visible to operators.

## Redis layout

The default local configuration uses one Redis instance with separate logical databases:

- DB 0: gateway coordination, authorization cache and public catalog cache
- DB 1: Celery broker
- DB 2: Celery result backend

Redis uses `noeviction` in Docker Compose. When memory is exhausted, writes fail instead of silently evicting rate-limit, concurrency, idempotency or broker keys.

## Operations API

Authenticated users can call:

- `GET /operations/status`

It reports catalog freshness, public catalog-cache readiness and sanitized background-job health. Provider identity and detailed internal errors are not exposed through this endpoint.

Administrators can additionally call:

- `GET /admin/jobs?limit=100`
- `GET /admin/audit?limit=100`

The React Status page consumes the general operations endpoint. Users with the `admin` role receive an Admin navigation item with responsive job-history and audit-trail views.

## Environment variables

Important Phase 5 settings:

```env
FREE_MODEL_SYNC_INTERVAL_MINUTES=10
CATALOG_RELOAD_INTERVAL_SECONDS=15
CATALOG_CACHE_TTL_SECONDS=1800

CELERY_BROKER_URL=redis://:password@redis:6379/1
CELERY_RESULT_BACKEND=redis://:password@redis:6379/2

USAGE_AGGREGATION_BATCH_SIZE=5000
USAGE_AGGREGATION_MAX_BATCHES=4

REQUEST_METADATA_RETENTION_DAYS=30
AUDIT_LOG_RETENTION_DAYS=90
JOB_RUN_RETENTION_DAYS=14
AUTH_TOKEN_RETENTION_DAYS=7
JOB_RUN_STALE_MINUTES=120
```

`OPENROUTER_API_KEY`, `DATABASE_URL` and `REDIS_URL` remain server-only settings and must never be exposed to the React application.
