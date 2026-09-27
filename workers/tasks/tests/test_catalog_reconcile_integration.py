from __future__ import annotations

import json
from uuid import uuid4

import psycopg
from redis import Redis

from catalog_sync import (
    CATALOG_CACHE_KEY,
    DiscoveredModel,
    _cache_public_catalog,
    _reconcile_catalog,
)
from settings import WorkerSettings


def test_catalog_reconcile_preserves_uuid_and_caches_only_public_metadata() -> None:
    settings = WorkerSettings.from_env()
    upstream_id = f"provider/integration-{uuid4().hex}"
    display_name = "Integration Free Model"

    models, reconciled = _reconcile_catalog(
        settings,
        [
            DiscoveredModel(
                upstream_id=upstream_id,
                display_name=display_name,
                context_length=8192,
                capabilities={"text": True, "streaming": True},
            )
        ],
    )
    assert reconciled is True

    with psycopg.connect(settings.database_url) as conn, conn.cursor() as cur:
        cur.execute(
            """
            SELECT id,public_id,context_length,active,is_free
            FROM models
            WHERE upstream_id=%s
            """,
            (upstream_id,),
        )
        first = cur.fetchone()

    assert first is not None
    model_uuid, public_id, context_length, active, is_free = first
    assert public_id.startswith("nexora/")
    assert context_length == 8192
    assert active is True
    assert is_free is True

    models_again, reconciled_again = _reconcile_catalog(
        settings,
        [
            DiscoveredModel(
                upstream_id=upstream_id,
                display_name="Renamed Integration Model",
                context_length=16384,
                capabilities={"text": True, "streaming": True},
            )
        ],
    )
    assert reconciled_again is True

    with psycopg.connect(settings.database_url) as conn, conn.cursor() as cur:
        cur.execute(
            "SELECT id,public_id,context_length FROM models WHERE upstream_id=%s",
            (upstream_id,),
        )
        second = cur.fetchone()

    assert second[0] == model_uuid
    assert second[1] == public_id
    assert second[2] == 16384

    assert _cache_public_catalog(settings, models_again) is True
    redis_client = Redis.from_url(settings.redis_url, decode_responses=True)
    try:
        cached = redis_client.get(CATALOG_CACHE_KEY)
    finally:
        redis_client.close()

    assert cached
    payload = json.loads(cached)
    serialized = json.dumps(payload).lower()
    assert upstream_id.lower() not in serialized
    assert "upstream_id" not in serialized
    assert "provider_key" not in serialized
    assert any(item["id"] == public_id for item in payload["data"])
