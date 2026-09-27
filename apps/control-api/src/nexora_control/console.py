from datetime import datetime
from uuid import UUID

from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel, Field
from psycopg.rows import dict_row

from nexora_control.auth import UserPrincipal, get_current_user, require_csrf
from nexora_control.config import Settings, get_settings
from nexora_control.database import get_connection


router = APIRouter(tags=["console"])


class ProjectCreate(BaseModel):
    name: str = Field(min_length=1, max_length=120)


class ProjectView(BaseModel):
    id: UUID
    name: str
    status: str
    created_at: datetime


@router.get("/projects", response_model=list[ProjectView])
def projects(current_user: UserPrincipal = Depends(get_current_user)) -> list[ProjectView]:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            SELECT id, name, status, created_at
            FROM projects
            WHERE user_id=%s
            ORDER BY created_at DESC
            """,
            (current_user.id,),
        )
        return [ProjectView(**row) for row in cur.fetchall()]


@router.post(
    "/projects",
    response_model=ProjectView,
    status_code=status.HTTP_201_CREATED,
    dependencies=[Depends(require_csrf)],
)
def create_project(
    payload: ProjectCreate,
    current_user: UserPrincipal = Depends(get_current_user),
) -> ProjectView:
    name = payload.name.strip()
    if not name:
        raise HTTPException(status_code=422, detail="Project name is required")

    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            INSERT INTO projects(user_id, name)
            VALUES(%s,%s)
            RETURNING id, name, status, created_at
            """,
            (current_user.id, name),
        )
        row = cur.fetchone()
        conn.commit()
        return ProjectView(**row)


@router.get("/usage")
def usage(current_user: UserPrincipal = Depends(get_current_user)) -> list[dict]:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            SELECT ud.usage_date, ud.api_key_id, ud.model_id, ud.requests,
                   ud.prompt_tokens, ud.completion_tokens
            FROM usage_daily ud
            JOIN api_keys k ON k.id = ud.api_key_id
            JOIN projects p ON p.id = k.project_id
            WHERE p.user_id=%s
            ORDER BY ud.usage_date DESC
            LIMIT 100
            """,
            (current_user.id,),
        )
        return cur.fetchall()


@router.get("/requests")
def requests(current_user: UserPrincipal = Depends(get_current_user)) -> list[dict]:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            SELECT ue.request_id, ue.api_key_id, ue.model_id, ue.status, ue.ttft_ms,
                   ue.latency_ms, ue.prompt_tokens, ue.completion_tokens, ue.created_at
            FROM usage_events ue
            JOIN api_keys k ON k.id = ue.api_key_id
            JOIN projects p ON p.id = k.project_id
            WHERE p.user_id=%s
            ORDER BY ue.created_at DESC
            LIMIT 100
            """,
            (current_user.id,),
        )
        return cur.fetchall()


@router.get("/settings")
def settings(
    current_user: UserPrincipal = Depends(get_current_user),
    app_settings: Settings = Depends(get_settings),
) -> dict:
    return {
        "profile": {
            "id": current_user.id,
            "email": current_user.email,
            "display_name": current_user.display_name,
            "role": current_user.role,
            "email_verified": current_user.email_verified,
        },
        "session": {
            "access_ttl_minutes": app_settings.access_token_ttl_minutes,
            "refresh_ttl_days": app_settings.refresh_token_ttl_days,
        },
    }
