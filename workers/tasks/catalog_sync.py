from __future__ import annotations

from dataclasses import dataclass
from decimal import Decimal, InvalidOperation
import hashlib
import json
import logging
import time
from typing import Any
from urllib.parse import urlparse

import httpx
import psycopg
from psycopg.rows import dict_row
from psycopg.types.json import Jsonb
from redis import Redis
from redis.exceptions import RedisError

from operations import sanitize_error
from settings import WorkerSettings


logger = logging.getLogger("nexora.worker.catalog")

PROVIDER_KEY = "openrouter"
AUTO_FREE_PUBLIC_ID = "auto-free"
AUTO_FREE_UPSTREAM_ID = "openrouter/free"
CATALOG_CACHE_KEY = "nxa:catalog:public:v1"
_MAX_CATALOG_BYTES = 4 << 20
_RETRYABLE_STATUS = {408, 429, 500, 502, 503, 504}


class CatalogSyncError(RuntimeError):
    pass


@dataclass(frozen=True)
class DiscoveredModel:
    upstream_id: str
    display_name: str
    context_length: int | None
    capabilities: dict[str, bool]


def _slug_model_name(value: str) -> str:
    value = value.strip().lower()
    chars: list[str] = []
    last_dash = False
    for char in value:
        if ("a" <= char <= "z") or ("0" <= char <= "9"):
            chars.append(char)
            last_dash = False
        elif chars and not last_dash:
            chars.append("-")
            last_dash = True

    result = "".join(chars).strip("-")
    parts = [part for part in result.split("-") if part and part != PROVIDER_KEY]
    result = "-".join(parts)
    if len(result) > 48:
        result = result[:48].strip("-")
    return result


def public_model_alias(display_name: str, upstream_id: str) -> str:
    base = _slug_model_name(display_name) or "model"
    digest = hashlib.sha256(upstream_id.strip().encode("utf-8")).hexdigest()[:12]
    return f"nexora/{base}-{digest}"


def _zero_price(value: Any) -> bool:
    if value is None:
        return False
    try:
        return Decimal(str(value).strip()) == 0
    except (InvalidOperation, ValueError):
        return False


def parse_free_models(payload: dict[str, Any]) -> list[DiscoveredModel]:
    raw_models = payload.get("data")
    if not isinstance(raw_models, list):
        raise CatalogSyncError("provider catalog response is missing a model list")

    discovered: list[DiscoveredModel] = []
    seen: set[str] = set()
    for item in raw_models:
        if not isinstance(item, dict):
            continue

        upstream_id = str(item.get("id") or "").strip()
        if not upstream_id or upstream_id == AUTO_FREE_UPSTREAM_ID or upstream_id in seen:
            continue

        pricing = item.get("pricing")
        architecture = item.get("architecture")
        if not isinstance(pricing, dict) or not isinstance(architecture, dict):
            continue

        output_modalities = architecture.get("output_modalities")
        if not isinstance(output_modalities, list) or "text" not in output_modalities:
            continue

        if not _zero_price(pricing.get("prompt")) or not _zero_price(pricing.get("completion")):
            continue

        raw_context = item.get("context_length")
        context_length: int | None = None
        if isinstance(raw_context, int) and raw_context > 0:
            context_length = raw_context
        elif isinstance(raw_context, float) and raw_context > 0:
            context_length = int(raw_context)

        display_name = str(item.get("name") or "").strip() or "Free Model"
        seen.add(upstream_id)
        discovered.append(
            DiscoveredModel(
                upstream_id=upstream_id,
                display_name=display_name,
                context_length=context_length,
                capabilities={"text": True, "streaming": True},
            )
        )

    return discovered


def _read_json_limited(response: httpx.Response) -> dict[str, Any]:
    body = bytearray()
    for chunk in response.iter_bytes():
        body.extend(chunk)
        if len(body) > _MAX_CATALOG_BYTES:
            raise CatalogSyncError("provider catalog response exceeded the size limit")
    try:
        parsed = json.loads(body)
    except json.JSONDecodeError as exc:
        raise CatalogSyncError("provider catalog response is invalid JSON") from exc
    if not isinstance(parsed, dict):
        raise CatalogSyncError("provider catalog response has an invalid shape")
    return parsed


def fetch_free_models(settings: WorkerSettings) -> list[DiscoveredModel]:
    if not settings.openrouter_api_key:
        raise CatalogSyncError("OPENROUTER_API_KEY is required for catalog synchronization")

    parsed_url = urlparse(settings.upstream_base_url)
    if parsed_url.scheme != "https" or not parsed_url.netloc:
        raise CatalogSyncError("UPSTREAM_BASE_URL must be a valid HTTPS URL")

    timeout = httpx.Timeout(
        settings.provider_catalog_timeout_seconds,
        connect=min(5.0, float(settings.provider_catalog_timeout_seconds)),
    )
    headers = {
        "Authorization": f"Bearer {settings.openrouter_api_key}",
        "Accept": "application/json",
    }

    with httpx.Client(
        timeout=timeout,
        follow_redirects=False,
        headers=headers,
        limits=httpx.Limits(max_keepalive_connections=5, max_connections=10),
    ) as client:
        last_error: Exception | None = None
        for attempt in range(1, settings.provider_max_attempts + 1):
            try:
                with client.stream("GET", f"{settings.upstream_base_url}/models") as response:
                    if 200 <= response.status_code < 300:
                        return parse_free_models(_read_json_limited(response))

                    if response.status_code not in _RETRYABLE_STATUS:
                        raise CatalogSyncError(
                            f"provider catalog request returned status {response.status_code}"
                        )
                    last_error = CatalogSyncError(
                        f"provider catalog request returned retryable status {response.status_code}"
                    )
            except (httpx.TimeoutException, httpx.TransportError) as exc:
                last_error = exc

            if attempt < settings.provider_max_attempts:
                time.sleep(min(1.5, 0.2 * (2 ** (attempt - 1))))

    raise CatalogSyncError(
        sanitize_error(last_error or CatalogSyncError("provider catalog request failed"))
    )


def _reconcile_catalog(
    settings: WorkerSettings,
    discovered: list[DiscoveredModel],
) -> tuple[list[dict[str, Any]], bool]:
    with psycopg.connect(settings.database_url) as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute("SELECT pg_try_advisory_xact_lock(hashtext('nexora.catalog.sync')) AS locked")
        lock_row = cur.fetchone()
        if not lock_row or not lock_row["locked"]:
            return [], False

        cur.execute(
            """
            INSERT INTO models (
                public_id, upstream_id, display_name, provider_key,
                context_length, active, is_free, capabilities
            )
            VALUES (
                %s,%s,'Auto Free',%s,NULL,true,true,%s
            )
            ON CONFLICT (public_id) DO UPDATE SET
                upstream_id=EXCLUDED.upstream_id,
                display_name=EXCLUDED.display_name,
                provider_key=EXCLUDED.provider_key,
                context_length=NULL,
                active=true,
                is_free=true,
                capabilities=EXCLUDED.capabilities,
                updated_at=now()
            """,
            (
                AUTO_FREE_PUBLIC_ID,
                AUTO_FREE_UPSTREAM_ID,
                PROVIDER_KEY,
                Jsonb({"text": True, "streaming": True}),
            ),
        )

        cur.execute(
            """
            UPDATE models
            SET active=false, updated_at=now()
            WHERE provider_key=%s
              AND upstream_id<>%s
              AND active=true
            """,
            (PROVIDER_KEY, AUTO_FREE_UPSTREAM_ID),
        )

        rows = [
            (
                public_model_alias(model.display_name, model.upstream_id),
                model.upstream_id,
                model.display_name,
                PROVIDER_KEY,
                model.context_length,
                Jsonb(model.capabilities),
            )
            for model in discovered
        ]
        if rows:
            cur.executemany(
                """
                INSERT INTO models (
                    public_id, upstream_id, display_name, provider_key,
                    context_length, active, is_free, capabilities
                )
                VALUES (%s,%s,%s,%s,%s,true,true,%s)
                ON CONFLICT (upstream_id) DO UPDATE SET
                    public_id=CASE
                        WHEN models.public_id='auto-free'
                          OR models.public_id LIKE 'nexora/%%'
                        THEN models.public_id
                        ELSE EXCLUDED.public_id
                    END,
                    display_name=EXCLUDED.display_name,
                    provider_key=EXCLUDED.provider_key,
                    context_length=EXCLUDED.context_length,
                    active=true,
                    is_free=true,
                    capabilities=EXCLUDED.capabilities,
                    updated_at=now()
                """,
                rows,
            )

        cur.execute(
            """
            SELECT public_id, display_name, context_length, capabilities
            FROM models
            WHERE active=true
              AND is_free=true
              AND (public_id='auto-free' OR public_id LIKE 'nexora/%%')
            ORDER BY
                CASE WHEN public_id='auto-free' THEN 0 ELSE 1 END,
                public_id
            """
        )
        public_models = [
            {
                "id": row["public_id"],
                "display_name": row["display_name"],
                "context_length": row["context_length"],
                "capabilities": row["capabilities"] or {},
                "free": True,
                "status": "active",
                "owned_by": "nexora",
                "object": "model",
            }
            for row in cur.fetchall()
        ]

        cur.execute(
            """
            INSERT INTO catalog_sync_state (
                provider_key, status, last_attempt_at, last_success_at,
                model_count, last_error, updated_at
            )
            VALUES (%s,'succeeded',now(),now(),%s,NULL,now())
            ON CONFLICT (provider_key) DO UPDATE SET
                status='succeeded',
                last_attempt_at=now(),
                last_success_at=now(),
                model_count=EXCLUDED.model_count,
                last_error=NULL,
                updated_at=now()
            """,
            (PROVIDER_KEY, len(public_models)),
        )
        conn.commit()
        return public_models, True


def _mark_catalog_failure(settings: WorkerSettings, error_message: str) -> None:
    try:
        with psycopg.connect(settings.database_url) as conn:
            conn.execute(
                """
                INSERT INTO catalog_sync_state (
                    provider_key, status, last_attempt_at, model_count,
                    last_error, updated_at
                )
                VALUES (%s,'failed',now(),0,%s,now())
                ON CONFLICT (provider_key) DO UPDATE SET
                    status='failed',
                    last_attempt_at=now(),
                    last_error=EXCLUDED.last_error,
                    updated_at=now()
                """,
                (PROVIDER_KEY, error_message),
            )
            conn.commit()
    except psycopg.Error as exc:
        logger.warning("unable to persist catalog sync failure: %s", sanitize_error(exc))


def _cache_public_catalog(settings: WorkerSettings, models: list[dict[str, Any]]) -> bool:
    try:
        client = Redis.from_url(
            settings.redis_url,
            decode_responses=True,
            socket_connect_timeout=2,
            socket_timeout=2,
            health_check_interval=30,
        )
        payload = json.dumps(
            {"object": "list", "data": models},
            separators=(",", ":"),
        )
        client.setex(CATALOG_CACHE_KEY, settings.catalog_cache_ttl_seconds, payload)
        client.close()
        return True
    except RedisError as exc:
        logger.warning("catalog Redis cache update failed: %s", sanitize_error(exc))
        return False


def sync_model_catalog(settings: WorkerSettings) -> dict[str, Any]:
    try:
        discovered = fetch_free_models(settings)
        public_models, reconciled = _reconcile_catalog(settings, discovered)
        if not reconciled:
            return {"skipped": True, "reason": "sync_already_running"}

        cache_updated = _cache_public_catalog(settings, public_models)
        return {
            "discovered_models": len(discovered),
            "active_models": len(public_models),
            "cache_updated": cache_updated,
        }
    except Exception as exc:
        error_message = sanitize_error(exc)
        _mark_catalog_failure(settings, error_message)
        raise
