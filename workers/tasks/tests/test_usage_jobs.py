from __future__ import annotations

from datetime import datetime, timedelta, timezone
from uuid import uuid4

import psycopg

from settings import WorkerSettings
from usage_jobs import aggregate_usage, cleanup_housekeeping


def _settings() -> WorkerSettings:
    base = WorkerSettings.from_env()
    return WorkerSettings(
        **{
            **base.__dict__,
            "usage_aggregation_batch_size": 100,
            "usage_aggregation_max_batches": 2,
            "request_metadata_retention_days": 30,
            "audit_log_retention_days": 90,
            "job_run_retention_days": 14,
            "auth_token_retention_days": 7,
        }
    )


def _seed_identity(database_url: str) -> tuple[str, str, str]:
    email = f"worker-{uuid4().hex}@example.com"
    with psycopg.connect(database_url) as conn, conn.cursor() as cur:
        cur.execute(
            """
            INSERT INTO users(email,password_hash,status,display_name,role,email_verified)
            VALUES (%s,'test-hash','active','Worker Test','user',true)
            RETURNING id
            """,
            (email,),
        )
        user_id = cur.fetchone()[0]
        cur.execute(
            "INSERT INTO projects(user_id,name,status) VALUES (%s,'Worker Project','active') RETURNING id",
            (user_id,),
        )
        project_id = cur.fetchone()[0]
        cur.execute(
            """
            INSERT INTO api_keys(
                project_id,name,key_prefix,key_hash,status,allow_all_free_models
            )
            VALUES (%s,'Worker Key',%s,%s,'active',true)
            RETURNING id
            """,
            (project_id, f"nxa_test_{uuid4().hex[:12]}", b"worker-test-hash"),
        )
        api_key_id = cur.fetchone()[0]
        conn.commit()
    return str(user_id), str(project_id), str(api_key_id)


def test_incremental_usage_aggregation_is_exactly_once() -> None:
    settings = _settings()
    _, _, api_key_id = _seed_identity(settings.database_url)

    with psycopg.connect(settings.database_url) as conn:
        conn.execute(
            """
            UPDATE usage_aggregation_state
            SET last_created_at='1970-01-01 00:00:00+00',
                last_event_id='00000000-0000-0000-0000-000000000000',
                updated_at=now()
            WHERE name='usage_daily'
            """
        )
        for index in range(3):
            conn.execute(
                """
                INSERT INTO usage_events(
                    request_id,api_key_id,status,prompt_tokens,completion_tokens
                )
                VALUES (%s,%s,200,%s,%s)
                """,
                (f"worker-usage-{uuid4().hex}", api_key_id, 10 + index, 5 + index),
            )
        conn.commit()

    first = aggregate_usage(settings)
    second = aggregate_usage(settings)

    assert first["events"] >= 3
    assert second["events"] == 0

    with psycopg.connect(settings.database_url) as conn, conn.cursor() as cur:
        cur.execute(
            """
            SELECT requests,prompt_tokens,completion_tokens
            FROM usage_daily
            WHERE api_key_id=%s
            """,
            (api_key_id,),
        )
        row = cur.fetchone()

    assert row == (3, 33, 18)


def test_housekeeping_removes_expired_metadata_in_bounded_batches() -> None:
    settings = _settings()
    user_id, _, api_key_id = _seed_identity(settings.database_url)
    old = datetime.now(timezone.utc) - timedelta(days=120)

    with psycopg.connect(settings.database_url) as conn:
        conn.execute(
            """
            INSERT INTO usage_events(request_id,api_key_id,status,created_at)
            VALUES (%s,%s,200,%s)
            """,
            (f"old-{uuid4().hex}", api_key_id, old),
        )
        conn.execute(
            """
            INSERT INTO audit_logs(actor_user_id,action,resource_type,metadata,created_at)
            VALUES (%s,'worker.old','worker_test','{}'::jsonb,%s)
            """,
            (user_id, old),
        )
        conn.execute(
            """
            INSERT INTO background_job_runs(
                job_name,status,started_at,completed_at,result
            )
            VALUES ('nexora.test','succeeded',%s,%s,'{}'::jsonb)
            """,
            (old, old),
        )
        conn.commit()

    result = cleanup_housekeeping(settings)

    assert result["deleted_usage_events"] >= 1
    assert result["deleted_audit_logs"] >= 1
    assert result["deleted_job_runs"] >= 1
