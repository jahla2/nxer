from __future__ import annotations

from dataclasses import dataclass
import os


def _int_env(name: str, default: int, *, minimum: int = 1, maximum: int | None = None) -> int:
    raw = os.getenv(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    if value < minimum:
        return default
    if maximum is not None and value > maximum:
        return default
    return value


@dataclass(frozen=True)
class WorkerSettings:
    database_url: str
    redis_url: str
    openrouter_api_key: str
    upstream_base_url: str
    free_model_sync_interval_minutes: int
    catalog_cache_ttl_seconds: int
    provider_catalog_timeout_seconds: int
    provider_max_attempts: int
    usage_aggregation_batch_size: int
    usage_aggregation_max_batches: int
    request_metadata_retention_days: int
    audit_log_retention_days: int
    job_run_retention_days: int
    auth_token_retention_days: int
    job_run_stale_minutes: int

    @classmethod
    def from_env(cls) -> "WorkerSettings":
        sync_minutes = _int_env("FREE_MODEL_SYNC_INTERVAL_MINUTES", 10, minimum=1, maximum=1440)
        return cls(
            database_url=os.getenv(
                "DATABASE_URL",
                "postgresql://nexora:nexora@postgres:5432/nexora",
            ),
            redis_url=os.getenv("REDIS_URL", "redis://:nexora@redis:6379/0"),
            openrouter_api_key=os.getenv("OPENROUTER_API_KEY", "").strip(),
            upstream_base_url=os.getenv(
                "UPSTREAM_BASE_URL",
                "https://openrouter.ai/api/v1",
            ).rstrip("/"),
            free_model_sync_interval_minutes=sync_minutes,
            catalog_cache_ttl_seconds=_int_env(
                "CATALOG_CACHE_TTL_SECONDS",
                max(900, sync_minutes * 60 * 3),
                minimum=60,
                maximum=86400,
            ),
            provider_catalog_timeout_seconds=_int_env(
                "PROVIDER_CATALOG_TIMEOUT_SECONDS",
                15,
                minimum=2,
                maximum=120,
            ),
            provider_max_attempts=_int_env(
                "PROVIDER_MAX_ATTEMPTS",
                2,
                minimum=1,
                maximum=4,
            ),
            usage_aggregation_batch_size=_int_env(
                "USAGE_AGGREGATION_BATCH_SIZE",
                5000,
                minimum=100,
                maximum=50000,
            ),
            usage_aggregation_max_batches=_int_env(
                "USAGE_AGGREGATION_MAX_BATCHES",
                4,
                minimum=1,
                maximum=20,
            ),
            request_metadata_retention_days=_int_env(
                "REQUEST_METADATA_RETENTION_DAYS",
                30,
                minimum=1,
                maximum=3650,
            ),
            audit_log_retention_days=_int_env(
                "AUDIT_LOG_RETENTION_DAYS",
                90,
                minimum=7,
                maximum=3650,
            ),
            job_run_retention_days=_int_env(
                "JOB_RUN_RETENTION_DAYS",
                14,
                minimum=1,
                maximum=365,
            ),
            auth_token_retention_days=_int_env(
                "AUTH_TOKEN_RETENTION_DAYS",
                7,
                minimum=1,
                maximum=365,
            ),
            job_run_stale_minutes=_int_env(
                "JOB_RUN_STALE_MINUTES",
                120,
                minimum=10,
                maximum=1440,
            ),
        )
