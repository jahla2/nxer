from datetime import datetime
from typing import Annotated
from uuid import UUID

from fastapi import APIRouter, Depends, Header, HTTPException, status
from pydantic import BaseModel, Field
from psycopg.rows import dict_row

from nexora_control.config import Settings, get_settings
from nexora_control.database import get_connection

router = APIRouter(tags=["console"])

def require_admin_token(
    authorization: Annotated[str | None, Header()] = None,
    settings: Settings = Depends(get_settings),
) -> None:
    expected = settings.control_admin_token
    if not expected:
        raise HTTPException(status_code=503, detail="Control API authentication is not configured")
    if authorization != f"Bearer {expected}":
        raise HTTPException(status_code=401, detail="Invalid control API credential")

class ProjectCreate(BaseModel):
    name: str = Field(min_length=1, max_length=120)

class ProjectView(BaseModel):
    id: UUID
    name: str
    status: str
    created_at: datetime

@router.get("/projects", response_model=list[ProjectView], dependencies=[Depends(require_admin_token)])
def projects() -> list[ProjectView]:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute("SELECT id,name,status,created_at FROM projects ORDER BY created_at DESC")
        return [ProjectView(**row) for row in cur.fetchall()]

@router.post("/projects", response_model=ProjectView, status_code=status.HTTP_201_CREATED, dependencies=[Depends(require_admin_token)])
def create_project(payload: ProjectCreate) -> ProjectView:
    name=payload.name.strip()
    if not name: raise HTTPException(status_code=422,detail="Project name is required")
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute("SELECT id FROM users WHERE status='active' ORDER BY created_at LIMIT 1")
        user=cur.fetchone()
        if user is None: raise HTTPException(status_code=409,detail="No local user exists. Run local bootstrap.")
        cur.execute("INSERT INTO projects(user_id,name) VALUES(%s,%s) RETURNING id,name,status,created_at",(user["id"],name))
        row=cur.fetchone(); conn.commit(); return ProjectView(**row)

@router.get("/usage", dependencies=[Depends(require_admin_token)])
def usage() -> list[dict]:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute("""SELECT usage_date, api_key_id, model_id, requests, prompt_tokens, completion_tokens
                       FROM usage_daily ORDER BY usage_date DESC LIMIT 100""")
        return cur.fetchall()

@router.get("/requests", dependencies=[Depends(require_admin_token)])
def requests() -> list[dict]:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute("""SELECT request_id, api_key_id, model_id, status, ttft_ms, latency_ms,
                              prompt_tokens, completion_tokens, created_at
                       FROM usage_events ORDER BY created_at DESC LIMIT 100""")
        return cur.fetchall()

@router.get("/settings", dependencies=[Depends(require_admin_token)])
def settings() -> list[dict]:
    with get_connection() as conn, conn.cursor(row_factory=dict_row) as cur:
        cur.execute("SELECT key,value,version,updated_at FROM system_settings ORDER BY key")
        return cur.fetchall()
