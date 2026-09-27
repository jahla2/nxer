from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

from fastapi import APIRouter, Depends, HTTPException, Query, status
from pydantic import BaseModel, Field
from psycopg.rows import dict_row
from redis.exceptions import RedisError

from nexora_control.auth import UserPrincipal, get_current_user
from nexora_control.config import Settings, get_settings
from nexora_control.database import get_connection
from nexora_control.gateway_cache import get_gateway_cache_client


router = APIRouter(tags=["operations"])

CATALOG_CACHE_KEY = "nxa:catalog:public:v1"
TRACKED_JOBS = (
    "nexora.sync_model_catalog",
    "nexora.aggregate_usage",
    "nexora.cleanup_housekeeping",
)


class CatalogStatusView(BaseModel):
    status: str
    last_attempt_at: datetime | None
    last_success_at: datetime | None
    model_count: int
    active_model_count: int
    stale: bool
    stale_after_minutes: int
    cache_available: bool
    cache_ttl_seconds: int | None


class JobStatusView(BaseModel):
    job_name: str
    status: str
    started_at: datetime | None
    completed_at: datetime | None
    duration_ms: int | None
    error_message: str | None


class OperationsStatusView(BaseModel):
    status: str
    catalog: CatalogStatusView
    jobs: list[JobStatusView]


class AdminJobRunView(BaseModel):
    id: str
    job_name: str
    task_id: str | None
    status: str
    started_at: datetime
    completed_at: datetime | None
    duration_ms: int | None
    result: dict[str, Any]
    error_message: str | None
    worker_name: str | None


class AuditLogView(BaseModel):
    id: str
    actor_user_id: str | None
    action: str
    resource_type: str
    resource_id: str | None
    metadata: dict[str, Any]
    created_at: datetime


def require_admin(
    current_user: UserPrincipal = Depends(get_current_user),
) -> UserPrincipal:
    if current_user.role != "admin":
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="Administrator access required",
        )
    return current_user


def _catalog_cache_status() -> tuple[bool, int | None]:
    try:
        client = get_gateway_cache_client()
        ttl = int(client.ttl(CATALOG_CACHE_KEY))
        if ttl < 0:
            return False, None
        return True, ttl
    except (RedisError, ValueError, TypeError):
        return False, None


@router.get("/operations/status", response_model=OperationsStatusView)
def operations_status(
    current_user: UserPrincipal = Depends(get_current_user),
    settings: Settings = Depends(get_settings),
) -> OperationsStatusView:
    del current_user

    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            SELECT
                provider_key,
                status,
                last_attempt_at,
                last_success_at,
                model_count
            FROM catalog_sync_state
            WHERE provider_key='openrouter'
            """
        )
        catalog_row = cur.fetchone() or {
            "provider_key": "openrouter",
            "status": "never",
            "last_attempt_at": None,
            "last_success_at": None,
            "model_count": 0,
        }

        cur.execute(
            """
            SELECT count(*) AS active_model_count
            FROM models
            WHERE active=true
              AND is_free=true
              AND (public_id='auto-free' OR public_id LIKE 'nexora/%')
            """
        )
        active_model_count = int(cur.fetchone()["active_model_count"])

        cur.execute(
            """
            SELECT DISTINCT ON (job_name)
                job_name,
                status,
                started_at,
                completed_at,
                duration_ms,
                error_message
            FROM background_job_runs
            WHERE job_name = ANY(%s)
            ORDER BY job_name, started_at DESC
            """,
            (list(TRACKED_JOBS),),
        )
        latest_by_name = {row["job_name"]: row for row in cur.fetchall()}

    now = datetime.now(timezone.utc)
    last_success = catalog_row["last_success_at"]
    stale_after_minutes = max(15, settings.free_model_sync_interval_minutes * 3)
    stale = (
        last_success is None
        or (now - last_success).total_seconds() > stale_after_minutes * 60
    )

    cache_available, cache_ttl = _catalog_cache_status()
    jobs = []
    for job_name in TRACKED_JOBS:
        row = latest_by_name.get(job_name)
        jobs.append(
            JobStatusView(
                job_name=job_name,
                status=row["status"] if row else "never",
                started_at=row["started_at"] if row else None,
                completed_at=row["completed_at"] if row else None,
                duration_ms=row["duration_ms"] if row else None,
                error_message=(
                    "Background job failed. Administrator review is required."
                    if row and row["status"] == "failed"
                    else None
                ),
            )
        )

    failed_jobs = any(item.status == "failed" for item in jobs)
    overall = "degraded" if stale or failed_jobs else "ok"

    return OperationsStatusView(
        status=overall,
        catalog=CatalogStatusView(
            status=catalog_row["status"],
            last_attempt_at=catalog_row["last_attempt_at"],
            last_success_at=last_success,
            model_count=int(catalog_row["model_count"] or 0),
            active_model_count=active_model_count,
            stale=stale,
            stale_after_minutes=stale_after_minutes,
            cache_available=cache_available,
            cache_ttl_seconds=cache_ttl,
        ),
        jobs=jobs,
    )


@router.get(
    "/admin/jobs",
    response_model=list[AdminJobRunView],
    dependencies=[Depends(require_admin)],
)
def admin_job_runs(
    limit: int = Query(default=100, ge=1, le=200),
) -> list[AdminJobRunView]:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            SELECT
                id::text AS id,
                job_name,
                task_id,
                status,
                started_at,
                completed_at,
                duration_ms,
                result,
                error_message,
                worker_name
            FROM background_job_runs
            ORDER BY started_at DESC
            LIMIT %s
            """,
            (limit,),
        )
        return [AdminJobRunView(**row) for row in cur.fetchall()]


@router.get(
    "/admin/audit",
    response_model=list[AuditLogView],
    dependencies=[Depends(require_admin)],
)
def admin_audit_logs(
    limit: int = Query(default=100, ge=1, le=200),
) -> list[AuditLogView]:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            SELECT
                id::text AS id,
                actor_user_id::text AS actor_user_id,
                action,
                resource_type,
                resource_id,
                metadata,
                created_at
            FROM audit_logs
            ORDER BY created_at DESC
            LIMIT %s
            """,
            (limit,),
        )
        return [AuditLogView(**row) for row in cur.fetchall()]
