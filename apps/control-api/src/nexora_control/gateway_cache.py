from functools import lru_cache
from typing import Iterable

from redis import Redis
from redis.exceptions import RedisError

from nexora_control.config import get_settings


AUTH_CACHE_PREFIX = "nxa:auth:key:"


class GatewayAuthCacheError(RuntimeError):
    pass


def api_key_cache_key(prefix: str) -> str:
    return f"{AUTH_CACHE_PREFIX}{prefix.strip()}"


@lru_cache
def get_gateway_cache_client() -> Redis:
    settings = get_settings()
    return Redis.from_url(
        settings.redis_url,
        decode_responses=False,
        socket_connect_timeout=2,
        socket_timeout=2,
        health_check_interval=30,
    )


def invalidate_api_key_prefixes(prefixes: Iterable[str]) -> None:
    keys = [api_key_cache_key(prefix) for prefix in prefixes if prefix and prefix.strip()]
    if not keys:
        return
    try:
        get_gateway_cache_client().delete(*keys)
    except RedisError as exc:
        raise GatewayAuthCacheError("gateway authorization cache is unavailable") from exc


def invalidate_api_key_prefixes_best_effort(prefixes: Iterable[str]) -> bool:
    try:
        invalidate_api_key_prefixes(prefixes)
        return True
    except GatewayAuthCacheError:
        return False
