from datetime import datetime
from typing import Annotated
from uuid import UUID

from fastapi import APIRouter, Depends, Header, HTTPException, status
from pydantic import BaseModel, Field
from psycopg.rows import dict_row

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


def require_admin_token(
    authorization: Annotated[str | None, Header()] = None,
    settings: Settings = Depends(get_settings),
) -> None:
    expected = settings.control_admin_token
    if not expected:
        raise HTTPException(status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="Control API authentication is not configured")
    if authorization != f"Bearer {expected}":
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Invalid control API credential")


@router.post("", response_model=APIKeyCreated, status_code=status.HTTP_201_CREATED, dependencies=[Depends(require_admin_token)])
def create_api_key(payload: APIKeyCreate, settings: Settings = Depends(get_settings)) -> APIKeyCreated:
    raw_key = generate_api_key()
    prefix = key_prefix(raw_key)
    digest = hash_api_key(raw_key, settings.api_key_hash_pepper)
    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute("SELECT 1 FROM projects WHERE id = %s AND status = 'active'", (payload.project_id,))
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
                    payload.project_id, payload.name.strip(), prefix, digest,
                    payload.allow_all_free_models, payload.requests_per_minute,
                    payload.requests_per_day, payload.max_concurrent, payload.expires_at,
                ),
            )
            row = cursor.fetchone()
        connection.commit()
    return APIKeyCreated(api_key=raw_key, **row)


@router.get("", response_model=list[APIKeyView], dependencies=[Depends(require_admin_token)])
def list_api_keys(project_id: UUID) -> list[APIKeyView]:
    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                """
                SELECT id, project_id, name, key_prefix, status, allow_all_free_models,
                       requests_per_minute, requests_per_day, max_concurrent,
                       created_at, last_used_at, expires_at, revoked_at
                FROM api_keys WHERE project_id = %s ORDER BY created_at DESC
                """,
                (project_id,),
            )
            return [APIKeyView(**row) for row in cursor.fetchall()]


@router.post("/{api_key_id}/revoke", response_model=APIKeyView, dependencies=[Depends(require_admin_token)])
def revoke_api_key(api_key_id: UUID) -> APIKeyView:
    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                """
                UPDATE api_keys SET status='revoked', revoked_at=COALESCE(revoked_at, now())
                WHERE id=%s
                RETURNING id, project_id, name, key_prefix, status, allow_all_free_models,
                          requests_per_minute, requests_per_day, max_concurrent,
                          created_at, last_used_at, expires_at, revoked_at
                """,
                (api_key_id,),
            )
            row = cursor.fetchone()
            if row is None:
                raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="API key not found")
        connection.commit()
    return APIKeyView(**row)
