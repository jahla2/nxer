from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

import psycopg
from psycopg.rows import dict_row

from settings import WorkerSettings


def aggregate_usage(settings: WorkerSettings) -> dict[str, Any]:
    total_events = 0
    total_dimensions = 0
    batches = 0
    caught_up = True

    with psycopg.connect(settings.database_url) as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute("SELECT pg_try_advisory_xact_lock(hashtext('nexora.usage.aggregate')) AS locked")
        lock_row = cur.fetchone()
        if not lock_row or not lock_row["locked"]:
            return {"skipped": True, "reason": "aggregation_already_running"}

        for _ in range(settings.usage_aggregation_max_batches):
            cur.execute(
                """
                WITH batch AS MATERIALIZED (
                    SELECT
                        id,
                        created_at,
                        api_key_id,
                        model_id,
                        prompt_tokens,
                        completion_tokens
                    FROM usage_events
                    WHERE aggregated_at IS NULL
                    ORDER BY created_at, id
                    FOR UPDATE SKIP LOCKED
                    LIMIT %s
                ),
                upserted AS (
                    INSERT INTO usage_daily (
                        usage_date,
                        api_key_id,
                        model_id,
                        requests,
                        prompt_tokens,
                        completion_tokens
                    )
                    SELECT
                        created_at::date,
                        api_key_id,
                        model_id,
                        count(*),
                        coalesce(sum(prompt_tokens), 0),
                        coalesce(sum(completion_tokens), 0)
                    FROM batch
                    GROUP BY created_at::date, api_key_id, model_id
                    ON CONFLICT (usage_date, api_key_id, model_id) DO UPDATE SET
                        requests=usage_daily.requests + EXCLUDED.requests,
                        prompt_tokens=usage_daily.prompt_tokens + EXCLUDED.prompt_tokens,
                        completion_tokens=usage_daily.completion_tokens + EXCLUDED.completion_tokens,
                        updated_at=now()
                    RETURNING 1
                ),
                marked AS (
                    UPDATE usage_events event
                    SET aggregated_at=now()
                    FROM batch
                    WHERE event.id=batch.id
                    RETURNING event.id
                )
                SELECT
                    (SELECT count(*) FROM batch) AS event_count,
                    (SELECT count(*) FROM upserted) AS dimension_count,
                    (SELECT count(*) FROM marked) AS marked_count
                """,
                (settings.usage_aggregation_batch_size,),
            )
            result = cur.fetchone()
            event_count = int(result["event_count"] or 0)
            marked_count = int(result["marked_count"] or 0)

            if event_count != marked_count:
                raise RuntimeError("usage aggregation did not mark every selected event")

            if event_count == 0:
                caught_up = True
                break

            total_events += event_count
            total_dimensions += int(result["dimension_count"] or 0)
            batches += 1
            caught_up = event_count < settings.usage_aggregation_batch_size

            if caught_up:
                break

        conn.commit()

    return {
        "events": total_events,
        "dimensions": total_dimensions,
        "batches": batches,
        "batch_size": settings.usage_aggregation_batch_size,
        "caught_up": caught_up,
    }


def _delete_batches(
    cur: psycopg.Cursor,
    sql: str,
    params: tuple[Any, ...],
    *,
    batch_size: int = 5000,
    max_batches: int = 20,
) -> int:
    deleted = 0
    for _ in range(max_batches):
        cur.execute(sql, (*params, batch_size))
        count = max(0, cur.rowcount)
        deleted += count
        if count < batch_size:
            break
    return deleted


def cleanup_housekeeping(settings: WorkerSettings) -> dict[str, Any]:
    now = datetime.now(timezone.utc)

    with psycopg.connect(settings.database_url) as conn, conn.cursor() as cur:
        cur.execute("SELECT pg_try_advisory_xact_lock(hashtext('nexora.housekeeping'))")
        locked = bool(cur.fetchone()[0])
        if not locked:
            return {"skipped": True, "reason": "housekeeping_already_running"}

        cur.execute(
            """
            UPDATE background_job_runs
            SET status='failed',
                completed_at=now(),
                duration_ms=LEAST(
                    2147483647,
                    GREATEST(
                        0,
                        floor(extract(epoch FROM (now() - started_at)) * 1000)
                    )
                )::integer,
                error_message=COALESCE(
                    error_message,
                    'Worker execution ended without a completion signal.'
                )
            WHERE status='running'
              AND started_at < now() - make_interval(mins => %s)
            """,
            (settings.job_run_stale_minutes,),
        )
        stale_jobs = max(0, cur.rowcount)

        deleted_idempotency = _delete_batches(
            cur,
            """
            WITH doomed AS (
                SELECT ctid
                FROM idempotency_records
                WHERE expires_at < now()
                LIMIT %s
            )
            DELETE FROM idempotency_records
            WHERE ctid IN (SELECT ctid FROM doomed)
            """,
            (),
        )

        # Never delete raw request metadata until the daily aggregate transaction
        # has marked the event as processed.
        deleted_usage_events = _delete_batches(
            cur,
            """
            WITH doomed AS (
                SELECT ctid
                FROM usage_events
                WHERE aggregated_at IS NOT NULL
                  AND created_at < now() - make_interval(days => %s)
                LIMIT %s
            )
            DELETE FROM usage_events
            WHERE ctid IN (SELECT ctid FROM doomed)
            """,
            (settings.request_metadata_retention_days,),
        )

        deleted_audit_logs = _delete_batches(
            cur,
            """
            WITH doomed AS (
                SELECT ctid
                FROM audit_logs
                WHERE created_at < now() - make_interval(days => %s)
                LIMIT %s
            )
            DELETE FROM audit_logs
            WHERE ctid IN (SELECT ctid FROM doomed)
            """,
            (settings.audit_log_retention_days,),
        )

        deleted_password_tokens = _delete_batches(
            cur,
            """
            WITH doomed AS (
                SELECT ctid
                FROM password_reset_tokens
                WHERE expires_at < now() - make_interval(days => %s)
                LIMIT %s
            )
            DELETE FROM password_reset_tokens
            WHERE ctid IN (SELECT ctid FROM doomed)
            """,
            (settings.auth_token_retention_days,),
        )

        deleted_verification_tokens = _delete_batches(
            cur,
            """
            WITH doomed AS (
                SELECT ctid
                FROM email_verification_tokens
                WHERE expires_at < now() - make_interval(days => %s)
                LIMIT %s
            )
            DELETE FROM email_verification_tokens
            WHERE ctid IN (SELECT ctid FROM doomed)
            """,
            (settings.auth_token_retention_days,),
        )

        deleted_job_runs = _delete_batches(
            cur,
            """
            WITH doomed AS (
                SELECT ctid
                FROM background_job_runs
                WHERE status IN ('succeeded','failed')
                  AND started_at < now() - make_interval(days => %s)
                LIMIT %s
            )
            DELETE FROM background_job_runs
            WHERE ctid IN (SELECT ctid FROM doomed)
            """,
            (settings.job_run_retention_days,),
        )

        conn.commit()

    return {
        "stale_jobs_marked_failed": stale_jobs,
        "deleted_idempotency": deleted_idempotency,
        "deleted_usage_events": deleted_usage_events,
        "deleted_audit_logs": deleted_audit_logs,
        "deleted_password_tokens": deleted_password_tokens,
        "deleted_verification_tokens": deleted_verification_tokens,
        "deleted_job_runs": deleted_job_runs,
        "completed_at": now.isoformat(),
    }
