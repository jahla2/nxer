from datetime import datetime
from uuid import UUID

from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel, Field
from psycopg.rows import dict_row

from nexora_control.audit import write_audit
from nexora_control.auth import UserPrincipal, get_current_user, require_csrf
from nexora_control.config import Settings, get_settings
from nexora_control.database import get_connection


router = APIRouter(tags=["console"])


class ProjectCreate(BaseModel):
    name: str = Field(min_length=1, max_length=120)


class ProjectUpdate(BaseModel):
    name: str = Field(min_length=1, max_length=120)


class ProjectView(BaseModel):
    id: UUID
    name: str
    status: str
    created_at: datetime
    updated_at: datetime


@router.get("/projects", response_model=list[ProjectView])
def projects(current_user: UserPrincipal = Depends(get_current_user)) -> list[ProjectView]:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            SELECT id, name, status, created_at, updated_at
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
            RETURNING id, name, status, created_at, updated_at
            """,
            (current_user.id, name),
        )
        row = cur.fetchone()
        write_audit(
            conn,
            actor_user_id=current_user.id,
            action="project.created",
            resource_type="project",
            resource_id=str(row["id"]),
            metadata={"name": row["name"]},
        )
        conn.commit()
        return ProjectView(**row)


@router.patch(
    "/projects/{project_id}",
    response_model=ProjectView,
    dependencies=[Depends(require_csrf)],
)
def rename_project(
    project_id: UUID,
    payload: ProjectUpdate,
    current_user: UserPrincipal = Depends(get_current_user),
) -> ProjectView:
    name = payload.name.strip()
    if not name:
        raise HTTPException(status_code=422, detail="Project name is required")

    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            UPDATE projects
            SET name=%s, updated_at=now()
            WHERE id=%s AND user_id=%s AND status='active'
            RETURNING id, name, status, created_at, updated_at
            """,
            (name, project_id, current_user.id),
        )
        row = cur.fetchone()
        if row is None:
            raise HTTPException(status_code=404, detail="Active project not found")
        write_audit(
            conn,
            actor_user_id=current_user.id,
            action="project.renamed",
            resource_type="project",
            resource_id=str(project_id),
            metadata={"name": name},
        )
        conn.commit()
        return ProjectView(**row)


@router.post(
    "/projects/{project_id}/archive",
    response_model=ProjectView,
    dependencies=[Depends(require_csrf)],
)
def archive_project(
    project_id: UUID,
    current_user: UserPrincipal = Depends(get_current_user),
) -> ProjectView:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute(
            """
            UPDATE projects
            SET status='archived', updated_at=now()
            WHERE id=%s AND user_id=%s AND status='active'
            RETURNING id, name, status, created_at, updated_at
            """,
            (project_id, current_user.id),
        )
        row = cur.fetchone()
        if row is None:
            raise HTTPException(status_code=404, detail="Active project not found")

        cur.execute(
            """
            UPDATE api_keys
            SET status='revoked', revoked_at=COALESCE(revoked_at, now()), updated_at=now()
            WHERE project_id=%s AND status='active'
            RETURNING id
            """,
            (project_id,),
        )
        revoked_ids = [str(item["id"]) for item in cur.fetchall()]

        write_audit(
            conn,
            actor_user_id=current_user.id,
            action="project.archived",
            resource_type="project",
            resource_id=str(project_id),
            metadata={"revoked_api_key_ids": revoked_ids},
        )
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
            SELECT ue.request_id, ue.api_key_id, ue.model_id, ue.public_model_id,
                   ue.status, ue.ttft_ms, ue.latency_ms, ue.prompt_tokens,
                   ue.completion_tokens, ue.error_class, ue.created_at
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
