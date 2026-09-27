from datetime import datetime, timezone
from uuid import UUID

from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel, Field
from psycopg.rows import dict_row

from nexora_control.auth import UserPrincipal, get_current_user, require_csrf
from nexora_control.config import Settings, get_settings
from nexora_control.database import get_connection
from nexora_control.security import generate_api_key, hash_api_key, key_prefix


router = APIRouter(prefix="/api-keys", tags=["api-keys"])


class APIKeyCreate(BaseModel):
    project_id: UUID
    name: str = Field(min_length=1, max_length=120)
    allow_all_free_models: bool = True
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
    allow_all_free_models: bool
    requests_per_minute: int | None
    requests_per_day: int | None
    max_concurrent: int | None
    created_at: datetime
    last_used_at: datetime | None
    expires_at: datetime | None
    revoked_at: datetime | None


class APIKeyCreated(APIKeyView):
    api_key: str


def _validate_expiry(expires_at: datetime | None) -> None:
    if expires_at is None:
        return
    value = expires_at
    if value.tzinfo is None:
        value = value.replace(tzinfo=timezone.utc)
    if value <= datetime.now(timezone.utc):
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail="expires_at must be in the future")


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

    raw_key = generate_api_key()
    prefix = key_prefix(raw_key)
    digest = hash_api_key(raw_key, settings.api_key_hash_pepper)

    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                """
                SELECT 1
                FROM projects
                WHERE id=%s AND user_id=%s AND status='active'
                """,
                (payload.project_id, current_user.id),
            )
            if cursor.fetchone() is None:
                raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Active project not found")

            cursor.execute(
                """
                INSERT INTO api_keys (
                    project_id, name, key_prefix, key_hash, allow_all_free_models,
                    requests_per_minute, requests_per_day, max_concurrent, expires_at
                ) VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s)
                RETURNING id, project_id, name, key_prefix, status, allow_all_free_models,
                          requests_per_minute, requests_per_day, max_concurrent,
                          created_at, last_used_at, expires_at, revoked_at
                """,
                (
                    payload.project_id,
                    name,
                    prefix,
                    digest,
                    payload.allow_all_free_models,
                    payload.requests_per_minute,
                    payload.requests_per_day,
                    payload.max_concurrent,
                    payload.expires_at,
                ),
            )
            row = cursor.fetchone()
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

            cursor.execute(
                """
                SELECT id, project_id, name, key_prefix, status, allow_all_free_models,
                       requests_per_minute, requests_per_day, max_concurrent,
                       created_at, last_used_at, expires_at, revoked_at
                FROM api_keys
                WHERE project_id=%s
                ORDER BY created_at DESC
                """,
                (project_id,),
            )
            return [APIKeyView(**row) for row in cursor.fetchall()]


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
                SET status='revoked', revoked_at=COALESCE(k.revoked_at, now())
                WHERE k.id=%s
                  AND EXISTS (
                      SELECT 1
                      FROM projects p
                      WHERE p.id=k.project_id
                        AND p.user_id=%s
                  )
                RETURNING id, project_id, name, key_prefix, status, allow_all_free_models,
                          requests_per_minute, requests_per_day, max_concurrent,
                          created_at, last_used_at, expires_at, revoked_at
                """,
                (api_key_id, current_user.id),
            )
            row = cursor.fetchone()
            if row is None:
                raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="API key not found")
        connection.commit()
    return APIKeyView(**row)
