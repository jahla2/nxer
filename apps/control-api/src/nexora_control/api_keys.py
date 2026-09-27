from datetime import datetime, timezone
from uuid import UUID

from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel, Field
from psycopg.rows import dict_row

from nexora_control.audit import write_audit
from nexora_control.auth import UserPrincipal, get_current_user, require_csrf
from nexora_control.config import Settings, get_settings
from nexora_control.database import get_connection
from nexora_control.gateway_cache import (
    GatewayAuthCacheError,
    invalidate_api_key_prefixes,
    invalidate_api_key_prefixes_best_effort,
)
from nexora_control.security import generate_api_key, hash_api_key, key_prefix


def _invalidate_gateway_auth_cache(prefixes: list[str]) -> None:
    try:
        invalidate_api_key_prefixes(prefixes)
    except GatewayAuthCacheError as exc:
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail="Gateway authorization cache is unavailable. No key changes were applied.",
        ) from exc


def _invalidate_gateway_auth_cache_after_commit(prefixes: list[str]) -> None:
    invalidate_api_key_prefixes_best_effort(prefixes)


router = APIRouter(prefix="/api-keys", tags=["api-keys"])

KEY_SELECT = """
    SELECT
        k.id, k.project_id, k.name, k.key_prefix, k.status,
        k.default_model_id, k.allow_all_free_models,
        k.requests_per_minute, k.requests_per_day, k.max_concurrent,
        k.created_at, k.updated_at, k.last_used_at, k.expires_at, k.revoked_at,
        k.rotated_from_id,
        COALESCE(
            (SELECT array_agg(s.model_id ORDER BY s.model_id)
             FROM api_key_model_scopes s
             WHERE s.api_key_id=k.id),
            '{}'::uuid[]
        ) AS model_ids
    FROM api_keys k
"""


class APIKeyCreate(BaseModel):
    project_id: UUID
    name: str = Field(min_length=1, max_length=120)
    allow_all_free_models: bool = True
    default_model_id: UUID | None = None
    model_ids: list[UUID] = Field(default_factory=list)
    requests_per_minute: int | None = Field(default=None, ge=1)
    requests_per_day: int | None = Field(default=None, ge=1)
    max_concurrent: int | None = Field(default=None, ge=1)
    expires_at: datetime | None = None


class APIKeyUpdate(BaseModel):
    name: str | None = Field(default=None, min_length=1, max_length=120)
    allow_all_free_models: bool | None = None
    default_model_id: UUID | None = None
    model_ids: list[UUID] | None = None
    requests_per_minute: int | None = Field(default=None, ge=1)
    requests_per_day: int | None = Field(default=None, ge=1)
    max_concurrent: int | None = Field(default=None, ge=1)
    expires_at: datetime | None = None


class APIKeyView(BaseModel):
    id: UUID
    project_id: UUID
    name: str
    key_prefix: str
    status: str
    default_model_id: UUID | None
    model_ids: list[UUID]
    allow_all_free_models: bool
    requests_per_minute: int | None
    requests_per_day: int | None
    max_concurrent: int | None
    created_at: datetime
    updated_at: datetime
    last_used_at: datetime | None
    expires_at: datetime | None
    revoked_at: datetime | None
    rotated_from_id: UUID | None


class APIKeyCreated(APIKeyView):
    api_key: str


def _validate_expiry(expires_at: datetime | None) -> None:
    if expires_at is None:
        return
    value = expires_at
    if value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    if value <= datetime.now(timezone.utc):
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_ENTITY,
            detail="expires_at must be in the future",
        )


def _assert_owned_active_project(cursor, project_id: UUID, user_id: str) -> None:
    cursor.execute(
        """
        SELECT 1
        FROM projects
        WHERE id=%s AND user_id=%s AND status='active'
        """,
        (project_id, user_id),
    )
    if cursor.fetchone() is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Active project not found")


def _validate_models(cursor, model_ids: set[UUID]) -> None:
    if not model_ids:
        return
    cursor.execute(
        """
        SELECT id
        FROM models
        WHERE id = ANY(%s)
          AND active=true
          AND is_free=true
        """,
        (list(model_ids),),
    )
    found = {row["id"] for row in cursor.fetchall()}
    if found != model_ids:
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_ENTITY,
            detail="One or more selected models are unavailable or not free",
        )


def _normalized_scopes(
    *,
    allow_all_free_models: bool,
    default_model_id: UUID | None,
    model_ids: list[UUID] | None,
) -> set[UUID]:
    scopes = set(model_ids or [])
    if default_model_id is not None:
        scopes.add(default_model_id)
    if not allow_all_free_models and not scopes:
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_ENTITY,
            detail="At least one model must be selected when allow_all_free_models is false",
        )
    return scopes


def _replace_scopes(cursor, api_key_id: UUID, scopes: set[UUID], allow_all_free_models: bool) -> None:
    cursor.execute("DELETE FROM api_key_model_scopes WHERE api_key_id=%s", (api_key_id,))
    if allow_all_free_models:
        return
    for model_id in sorted(scopes, key=str):
        cursor.execute(
            "INSERT INTO api_key_model_scopes(api_key_id, model_id) VALUES (%s,%s)",
            (api_key_id, model_id),
        )


def _fetch_key(cursor, api_key_id: UUID) -> dict:
    cursor.execute(KEY_SELECT + " WHERE k.id=%s", (api_key_id,))
    row = cursor.fetchone()
    if row is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="API key not found")
    return row


@router.post(
    "",
    response_model=APIKeyCreated,
    status_code=status.HTTP_201_CREATED,
    dependencies=[Depends(require_csrf)],
)
def create_api_key(
    payload: APIKeyCreate,
    current_user: UserPrincipal = Depends(get_current_user),
    settings: Settings = Depends(get_settings),
) -> APIKeyCreated:
    name = payload.name.strip()
    if not name:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail="API key name is required")
    _validate_expiry(payload.expires_at)
    scopes = _normalized_scopes(
        allow_all_free_models=payload.allow_all_free_models,
        default_model_id=payload.default_model_id,
        model_ids=payload.model_ids,
    )

    raw_key = generate_api_key()
    prefix = key_prefix(raw_key)
    digest = hash_api_key(raw_key, settings.api_key_hash_pepper)

    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            _assert_owned_active_project(cursor, payload.project_id, current_user.id)
            _validate_models(cursor, scopes)

            cursor.execute(
                """
                INSERT INTO api_keys (
                    project_id, name, key_prefix, key_hash, default_model_id,
                    allow_all_free_models, requests_per_minute, requests_per_day,
                    max_concurrent, expires_at
                ) VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)
                RETURNING id
                """,
                (
                    payload.project_id,
                    name,
                    prefix,
                    digest,
                    payload.default_model_id,
                    payload.allow_all_free_models,
                    payload.requests_per_minute,
                    payload.requests_per_day,
                    payload.max_concurrent,
                    payload.expires_at,
                ),
            )
            api_key_id = cursor.fetchone()["id"]
            _replace_scopes(cursor, api_key_id, scopes, payload.allow_all_free_models)
            row = _fetch_key(cursor, api_key_id)

            write_audit(
                connection,
                actor_user_id=current_user.id,
                action="api_key.created",
                resource_type="api_key",
                resource_id=str(api_key_id),
                metadata={
                    "project_id": str(payload.project_id),
                    "key_prefix": prefix,
                    "allow_all_free_models": payload.allow_all_free_models,
                },
            )
        connection.commit()

    return APIKeyCreated(api_key=raw_key, **row)


@router.get("", response_model=list[APIKeyView])
def list_api_keys(
    project_id: UUID,
    current_user: UserPrincipal = Depends(get_current_user),
) -> list[APIKeyView]:
    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                "SELECT 1 FROM projects WHERE id=%s AND user_id=%s",
                (project_id, current_user.id),
            )
            if cursor.fetchone() is None:
                raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Project not found")

            cursor.execute(KEY_SELECT + " WHERE k.project_id=%s ORDER BY k.created_at DESC", (project_id,))
            return [APIKeyView(**row) for row in cursor.fetchall()]


@router.patch(
    "/{api_key_id}",
    response_model=APIKeyView,
    dependencies=[Depends(require_csrf)],
)
def update_api_key(
    api_key_id: UUID,
    payload: APIKeyUpdate,
    current_user: UserPrincipal = Depends(get_current_user),
) -> APIKeyView:
    fields = payload.model_fields_set
    if not fields:
        raise HTTPException(status_code=422, detail="No API key fields were provided")

    if "name" in fields:
        if payload.name is None or not payload.name.strip():
            raise HTTPException(status_code=422, detail="API key name is required")
    if "expires_at" in fields:
        _validate_expiry(payload.expires_at)

    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                KEY_SELECT
                + """
                  JOIN projects p ON p.id=k.project_id
                  WHERE k.id=%s AND p.user_id=%s
                """,
                (api_key_id, current_user.id),
            )
            existing = cursor.fetchone()
            if existing is None:
                raise HTTPException(status_code=404, detail="API key not found")
            if existing["status"] != "active":
                raise HTTPException(status_code=409, detail="Only active API keys can be updated")

            _invalidate_gateway_auth_cache([existing["key_prefix"]])

            allow_all = (
                payload.allow_all_free_models
                if "allow_all_free_models" in fields
                else existing["allow_all_free_models"]
            )
            default_model = (
                payload.default_model_id
                if "default_model_id" in fields
                else existing["default_model_id"]
            )
            model_ids = (
                payload.model_ids
                if "model_ids" in fields
                else list(existing["model_ids"])
            )
            scopes = _normalized_scopes(
                allow_all_free_models=allow_all,
                default_model_id=default_model,
                model_ids=model_ids,
            )
            _validate_models(cursor, scopes)

            name = payload.name.strip() if "name" in fields and payload.name is not None else existing["name"]
            rpm = payload.requests_per_minute if "requests_per_minute" in fields else existing["requests_per_minute"]
            daily = payload.requests_per_day if "requests_per_day" in fields else existing["requests_per_day"]
            concurrent = payload.max_concurrent if "max_concurrent" in fields else existing["max_concurrent"]
            expires_at = payload.expires_at if "expires_at" in fields else existing["expires_at"]

            cursor.execute(
                """
                UPDATE api_keys
                SET name=%s,
                    default_model_id=%s,
                    allow_all_free_models=%s,
                    requests_per_minute=%s,
                    requests_per_day=%s,
                    max_concurrent=%s,
                    expires_at=%s,
                    updated_at=now()
                WHERE id=%s
                """,
                (
                    name,
                    default_model,
                    allow_all,
                    rpm,
                    daily,
                    concurrent,
                    expires_at,
                    api_key_id,
                ),
            )
            _replace_scopes(cursor, api_key_id, scopes, allow_all)
            row = _fetch_key(cursor, api_key_id)

            write_audit(
                connection,
                actor_user_id=current_user.id,
                action="api_key.updated",
                resource_type="api_key",
                resource_id=str(api_key_id),
                metadata={"fields": sorted(fields)},
            )
        connection.commit()
    _invalidate_gateway_auth_cache_after_commit([existing["key_prefix"]])
    return APIKeyView(**row)


@router.post(
    "/{api_key_id}/rotate",
    response_model=APIKeyCreated,
    status_code=status.HTTP_201_CREATED,
    dependencies=[Depends(require_csrf)],
)
def rotate_api_key(
    api_key_id: UUID,
    current_user: UserPrincipal = Depends(get_current_user),
    settings: Settings = Depends(get_settings),
) -> APIKeyCreated:
    raw_key = generate_api_key()
    prefix = key_prefix(raw_key)
    digest = hash_api_key(raw_key, settings.api_key_hash_pepper)

    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                KEY_SELECT
                + """
                  JOIN projects p ON p.id=k.project_id
                  WHERE k.id=%s AND p.user_id=%s AND p.status='active'
                  FOR UPDATE OF k
                """,
                (api_key_id, current_user.id),
            )
            existing = cursor.fetchone()
            if existing is None:
                raise HTTPException(status_code=404, detail="Active API key not found")
            if existing["status"] != "active":
                raise HTTPException(status_code=409, detail="Only active API keys can be rotated")

            _invalidate_gateway_auth_cache([existing["key_prefix"]])

            cursor.execute(
                """
                INSERT INTO api_keys (
                    project_id, name, key_prefix, key_hash, default_model_id,
                    allow_all_free_models, requests_per_minute, requests_per_day,
                    max_concurrent, expires_at, rotated_from_id
                ) VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)
                RETURNING id
                """,
                (
                    existing["project_id"],
                    existing["name"],
                    prefix,
                    digest,
                    existing["default_model_id"],
                    existing["allow_all_free_models"],
                    existing["requests_per_minute"],
                    existing["requests_per_day"],
                    existing["max_concurrent"],
                    existing["expires_at"],
                    api_key_id,
                ),
            )
            new_id = cursor.fetchone()["id"]

            for model_id in existing["model_ids"]:
                cursor.execute(
                    "INSERT INTO api_key_model_scopes(api_key_id, model_id) VALUES (%s,%s)",
                    (new_id, model_id),
                )

            cursor.execute(
                """
                UPDATE api_keys
                SET status='revoked',
                    revoked_at=COALESCE(revoked_at, now()),
                    updated_at=now()
                WHERE id=%s
                """,
                (api_key_id,),
            )

            row = _fetch_key(cursor, new_id)
            write_audit(
                connection,
                actor_user_id=current_user.id,
                action="api_key.rotated",
                resource_type="api_key",
                resource_id=str(new_id),
                metadata={
                    "rotated_from_id": str(api_key_id),
                    "old_key_prefix": existing["key_prefix"],
                    "new_key_prefix": prefix,
                },
            )
        connection.commit()

    _invalidate_gateway_auth_cache_after_commit([existing["key_prefix"]])
    return APIKeyCreated(api_key=raw_key, **row)


@router.post(
    "/{api_key_id}/revoke",
    response_model=APIKeyView,
    dependencies=[Depends(require_csrf)],
)
def revoke_api_key(
    api_key_id: UUID,
    current_user: UserPrincipal = Depends(get_current_user),
) -> APIKeyView:
    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                """
                UPDATE api_keys k
                SET status='revoked',
                    revoked_at=COALESCE(k.revoked_at, now()),
                    updated_at=now()
                WHERE k.id=%s
                  AND EXISTS (
                      SELECT 1
                      FROM projects p
                      WHERE p.id=k.project_id
                        AND p.user_id=%s
                  )
                RETURNING k.id
                """,
                (api_key_id, current_user.id),
            )
            updated = cursor.fetchone()
            if updated is None:
                raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="API key not found")

            row = _fetch_key(cursor, api_key_id)
            write_audit(
                connection,
                actor_user_id=current_user.id,
                action="api_key.revoked",
                resource_type="api_key",
                resource_id=str(api_key_id),
                metadata={"key_prefix": row["key_prefix"]},
            )
        connection.commit()
    return APIKeyView(**row)
