import asyncio
from datetime import datetime
import json
from uuid import UUID

import httpx
from fastapi import APIRouter, Depends, HTTPException, status
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field
from psycopg.rows import dict_row

from nexora_control.auth import UserPrincipal, get_current_user, require_csrf
from nexora_control.config import Settings, get_settings
from nexora_control.database import get_connection


router = APIRouter(prefix="/playground", tags=["playground"])


class PlaygroundSessionCreate(BaseModel):
    project_id: UUID
    model_id: UUID | None = None


class PlaygroundSessionUpdate(BaseModel):
    title: str = Field(min_length=1, max_length=160)


class PlaygroundMessageCreate(BaseModel):
    content: str = Field(min_length=1, max_length=16000)
    model_id: UUID


class PlaygroundSessionView(BaseModel):
    id: UUID
    project_id: UUID
    title: str
    selected_model_id: UUID | None
    selected_model_public_id: str | None
    selected_model_display_name: str | None
    preview: str | None = None
    created_at: datetime
    updated_at: datetime


class PlaygroundMessageView(BaseModel):
    id: UUID
    session_id: UUID
    role: str
    content: str
    model_id: UUID | None
    model_public_id: str | None
    model_display_name: str | None
    request_id: str | None
    status: int | None
    ttft_ms: int | None
    latency_ms: int | None
    prompt_tokens: int | None
    completion_tokens: int | None
    created_at: datetime


class PlaygroundConversationView(BaseModel):
    session: PlaygroundSessionView
    messages: list[PlaygroundMessageView]


class PlaygroundTurnView(BaseModel):
    session: PlaygroundSessionView
    user_message: PlaygroundMessageView
    assistant_message: PlaygroundMessageView


SESSION_SELECT = """
    SELECT
        s.id,
        s.project_id,
        s.title,
        s.selected_model_id,
        m.public_id AS selected_model_public_id,
        m.display_name AS selected_model_display_name,
        (
            SELECT left(pm.content, 120)
            FROM playground_messages pm
            WHERE pm.session_id=s.id
            ORDER BY pm.created_at DESC, pm.id DESC
            LIMIT 1
        ) AS preview,
        s.created_at,
        s.updated_at
    FROM playground_sessions s
    LEFT JOIN models m ON m.id=s.selected_model_id
"""


MESSAGE_SELECT = """
    SELECT
        pm.id,
        pm.session_id,
        pm.role,
        pm.content,
        pm.model_id,
        m.public_id AS model_public_id,
        m.display_name AS model_display_name,
        pm.request_id,
        pm.status,
        pm.ttft_ms,
        pm.latency_ms,
        pm.prompt_tokens,
        pm.completion_tokens,
        pm.created_at
    FROM playground_messages pm
    LEFT JOIN models m ON m.id=pm.model_id
"""


def _session_row(cursor, session_id: UUID, user_id: str, *, active_project: bool = False) -> dict:
    project_status = "AND p.status='active'" if active_project else ""
    cursor.execute(
        SESSION_SELECT
        + f"""
          JOIN projects p ON p.id=s.project_id
          WHERE s.id=%s
            AND s.user_id=%s
            AND s.archived_at IS NULL
            {project_status}
        """,
        (session_id, user_id),
    )
    row = cursor.fetchone()
    if row is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Playground session not found")
    return row


def _resolve_model(cursor, model_id: UUID | None) -> dict:
    if model_id is None:
        cursor.execute(
            """
            SELECT id, public_id, display_name
            FROM models
            WHERE public_id='auto-free' AND active=true AND is_free=true
            LIMIT 1
            """
        )
    else:
        cursor.execute(
            """
            SELECT id, public_id, display_name
            FROM models
            WHERE id=%s
              AND active=true
              AND is_free=true
              AND (public_id='auto-free' OR public_id LIKE 'nexora/%%')
            LIMIT 1
            """,
            (model_id,),
        )
    row = cursor.fetchone()
    if row is None:
        raise HTTPException(
            status_code=status.HTTP_422_UNPROCESSABLE_ENTITY,
            detail="Selected model is unavailable",
        )
    return row


def _message_row(cursor, message_id: UUID) -> dict:
    cursor.execute(MESSAGE_SELECT + " WHERE pm.id=%s", (message_id,))
    row = cursor.fetchone()
    if row is None:
        raise HTTPException(status_code=status.HTTP_500_INTERNAL_SERVER_ERROR, detail="Playground message was not saved")
    return row


def _title_from_prompt(content: str) -> str:
    title = " ".join(content.strip().split())
    if len(title) > 64:
        title = title[:61].rstrip() + "..."
    return title or "New chat"


def _gateway_error(response: httpx.Response) -> str:
    try:
        body = response.json()
    except ValueError:
        return "The model request failed."
    if isinstance(body, dict):
        detail = body.get("detail")
        if isinstance(detail, str) and detail:
            return detail
        error = body.get("error")
        if isinstance(error, dict):
            message = error.get("message")
            if isinstance(message, str) and message:
                return message
    return "The model request failed."


def _call_gateway(
    settings: Settings,
    *,
    user_id: str,
    project_id: str,
    session_id: str,
    model: str,
    messages: list[dict[str, str]],
) -> dict:
    url = settings.gateway_internal_url.rstrip("/") + "/internal/playground/chat"
    try:
        response = httpx.post(
            url,
            json={
                "user_id": user_id,
                "project_id": project_id,
                "session_id": session_id,
                "model": model,
                "messages": messages,
            },
            headers={"X-Nexora-Internal-Token": settings.playground_internal_token},
            timeout=settings.playground_gateway_timeout_seconds,
        )
    except httpx.TimeoutException as exc:
        raise HTTPException(status_code=status.HTTP_504_GATEWAY_TIMEOUT, detail="The model request timed out") from exc
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="The model gateway is unavailable") from exc

    if response.status_code >= 400:
        raise HTTPException(status_code=response.status_code, detail=_gateway_error(response))

    payload = response.json()
    if not isinstance(payload, dict) or not isinstance(payload.get("message"), dict):
        raise HTTPException(status_code=status.HTTP_502_BAD_GATEWAY, detail="The model gateway returned an invalid response")
    return payload


@router.get("/sessions", response_model=list[PlaygroundSessionView])
def list_sessions(
    project_id: UUID,
    current_user: UserPrincipal = Depends(get_current_user),
) -> list[PlaygroundSessionView]:
    with get_connection() as connection, connection.cursor(row_factory=dict_row) as cursor:
        cursor.execute(
            "SELECT 1 FROM projects WHERE id=%s AND user_id=%s AND status='active'",
            (project_id, current_user.id),
        )
        if cursor.fetchone() is None:
            raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Active project not found")

        cursor.execute(
            SESSION_SELECT
            + """
              WHERE s.user_id=%s
                AND s.project_id=%s
                AND s.archived_at IS NULL
              ORDER BY s.updated_at DESC
              LIMIT 100
            """,
            (current_user.id, project_id),
        )
        return [PlaygroundSessionView(**row) for row in cursor.fetchall()]


@router.post(
    "/sessions",
    response_model=PlaygroundSessionView,
    status_code=status.HTTP_201_CREATED,
    dependencies=[Depends(require_csrf)],
)
def create_session(
    payload: PlaygroundSessionCreate,
    current_user: UserPrincipal = Depends(get_current_user),
) -> PlaygroundSessionView:
    with get_connection() as connection, connection.cursor(row_factory=dict_row) as cursor:
        cursor.execute(
            "SELECT 1 FROM projects WHERE id=%s AND user_id=%s AND status='active'",
            (payload.project_id, current_user.id),
        )
        if cursor.fetchone() is None:
            raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Active project not found")

        model = _resolve_model(cursor, payload.model_id)
        cursor.execute(
            """
            INSERT INTO playground_sessions(user_id, project_id, selected_model_id)
            VALUES (%s,%s,%s)
            RETURNING id
            """,
            (current_user.id, payload.project_id, model["id"]),
        )
        session_id = cursor.fetchone()["id"]
        connection.commit()
        row = _session_row(cursor, session_id, current_user.id)
        return PlaygroundSessionView(**row)


@router.get("/sessions/{session_id}", response_model=PlaygroundConversationView)
def get_session(
    session_id: UUID,
    current_user: UserPrincipal = Depends(get_current_user),
) -> PlaygroundConversationView:
    with get_connection() as connection, connection.cursor(row_factory=dict_row) as cursor:
        session = _session_row(cursor, session_id, current_user.id)
        cursor.execute(
            MESSAGE_SELECT
            + """
              WHERE pm.session_id=%s
              ORDER BY pm.created_at, pm.id
              LIMIT 500
            """,
            (session_id,),
        )
        return PlaygroundConversationView(
            session=PlaygroundSessionView(**session),
            messages=[PlaygroundMessageView(**row) for row in cursor.fetchall()],
        )


@router.patch(
    "/sessions/{session_id}",
    response_model=PlaygroundSessionView,
    dependencies=[Depends(require_csrf)],
)
def rename_session(
    session_id: UUID,
    payload: PlaygroundSessionUpdate,
    current_user: UserPrincipal = Depends(get_current_user),
) -> PlaygroundSessionView:
    title = " ".join(payload.title.strip().split())
    if not title:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail="Title is required")

    with get_connection() as connection, connection.cursor(row_factory=dict_row) as cursor:
        _session_row(cursor, session_id, current_user.id)
        cursor.execute(
            """
            UPDATE playground_sessions
            SET title=%s, updated_at=now()
            WHERE id=%s AND user_id=%s AND archived_at IS NULL
            """,
            (title, session_id, current_user.id),
        )
        connection.commit()
        return PlaygroundSessionView(**_session_row(cursor, session_id, current_user.id))


@router.delete(
    "/sessions/{session_id}",
    status_code=status.HTTP_204_NO_CONTENT,
    dependencies=[Depends(require_csrf)],
)
def delete_session(
    session_id: UUID,
    current_user: UserPrincipal = Depends(get_current_user),
) -> None:
    with get_connection() as connection:
        result = connection.execute(
            """
            DELETE FROM playground_sessions
            WHERE id=%s AND user_id=%s
            """,
            (session_id, current_user.id),
        )
        if result.rowcount == 0:
            raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Playground session not found")
        connection.commit()


@router.post(
    "/sessions/{session_id}/messages",
    response_model=PlaygroundTurnView,
    dependencies=[Depends(require_csrf)],
)
def send_message(
    session_id: UUID,
    payload: PlaygroundMessageCreate,
    current_user: UserPrincipal = Depends(get_current_user),
    settings: Settings = Depends(get_settings),
) -> PlaygroundTurnView:
    content = payload.content.strip()
    if not content:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail="Message is required")

    with get_connection() as connection, connection.cursor(row_factory=dict_row) as cursor:
        session = _session_row(cursor, session_id, current_user.id, active_project=True)
        model = _resolve_model(cursor, payload.model_id)

        cursor.execute(
            """
            INSERT INTO playground_messages(session_id, role, content, model_id)
            VALUES (%s,'user',%s,%s)
            RETURNING id
            """,
            (session_id, content, model["id"]),
        )
        user_message_id = cursor.fetchone()["id"]

        next_title = session["title"]
        if next_title == "New chat":
            cursor.execute(
                "SELECT count(*) FROM playground_messages WHERE session_id=%s AND role='user'",
                (session_id,),
            )
            if cursor.fetchone()["count"] == 1:
                next_title = _title_from_prompt(content)

        cursor.execute(
            """
            UPDATE playground_sessions
            SET title=%s, selected_model_id=%s, updated_at=now()
            WHERE id=%s
            """,
            (next_title, model["id"], session_id),
        )
        connection.commit()

        cursor.execute(
            """
            SELECT role, content
            FROM (
                SELECT role, content, created_at, id
                FROM playground_messages
                WHERE session_id=%s
                ORDER BY created_at DESC, id DESC
                LIMIT 40
            ) recent
            ORDER BY created_at, id
            """,
            (session_id,),
        )
        history = [{"role": row["role"], "content": row["content"]} for row in cursor.fetchall()]
        user_message = _message_row(cursor, user_message_id)

    gateway = _call_gateway(
        settings,
        user_id=current_user.id,
        project_id=str(session["project_id"]),
        session_id=str(session_id),
        model=model["public_id"],
        messages=history,
    )

    message = gateway["message"]
    assistant_content = str(message.get("content") or "").strip()
    if not assistant_content:
        assistant_content = "The model returned an empty response."
    metrics = gateway.get("metrics") if isinstance(gateway.get("metrics"), dict) else {}
    request_id = str(gateway.get("request_id") or "") or None

    with get_connection() as connection, connection.cursor(row_factory=dict_row) as cursor:
        cursor.execute(
            """
            INSERT INTO playground_messages(
                session_id, role, content, model_id, request_id, status,
                ttft_ms, latency_ms, prompt_tokens, completion_tokens
            )
            VALUES (%s,'assistant',%s,%s,%s,%s,%s,%s,%s,%s)
            RETURNING id
            """,
            (
                session_id,
                assistant_content,
                model["id"],
                request_id,
                metrics.get("status"),
                metrics.get("ttft_ms"),
                metrics.get("latency_ms"),
                metrics.get("prompt_tokens"),
                metrics.get("completion_tokens"),
            ),
        )
        assistant_message_id = cursor.fetchone()["id"]
        cursor.execute("UPDATE playground_sessions SET updated_at=now() WHERE id=%s", (session_id,))
        connection.commit()

        session_row = _session_row(cursor, session_id, current_user.id)
        assistant_message = _message_row(cursor, assistant_message_id)
        user_message = _message_row(cursor, user_message_id)

    return PlaygroundTurnView(
        session=PlaygroundSessionView(**session_row),
        user_message=PlaygroundMessageView(**user_message),
        assistant_message=PlaygroundMessageView(**assistant_message),
    )


def _sse(event: str, payload: dict) -> str:
    return f"event: {event}\ndata: {json.dumps(payload, separators=(',', ':'))}\n\n"


def _persist_stream_assistant(
    *,
    session_id: UUID,
    user_id: str,
    model_id: UUID,
    content: str,
    request_id: str | None,
    metrics: dict,
) -> None:
    with get_connection() as connection, connection.cursor(row_factory=dict_row) as cursor:
        # Re-check ownership before persisting the model response. This also
        # prevents a deleted session from being resurrected by a slow stream.
        _session_row(cursor, session_id, user_id)
        cursor.execute(
            """
            INSERT INTO playground_messages(
                session_id, role, content, model_id, request_id, status,
                ttft_ms, latency_ms, prompt_tokens, completion_tokens
            )
            VALUES (%s,'assistant',%s,%s,%s,%s,%s,%s,%s,%s)
            """,
            (
                session_id,
                content,
                model_id,
                request_id,
                metrics.get("status"),
                metrics.get("ttft_ms"),
                metrics.get("latency_ms"),
                metrics.get("prompt_tokens"),
                metrics.get("completion_tokens"),
            ),
        )
        cursor.execute("UPDATE playground_sessions SET updated_at=now() WHERE id=%s", (session_id,))
        connection.commit()


@router.post(
    "/sessions/{session_id}/stream",
    dependencies=[Depends(require_csrf)],
)
def stream_message(
    session_id: UUID,
    payload: PlaygroundMessageCreate,
    current_user: UserPrincipal = Depends(get_current_user),
    settings: Settings = Depends(get_settings),
) -> StreamingResponse:
    content = payload.content.strip()
    if not content:
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail="Message is required")

    with get_connection() as connection, connection.cursor(row_factory=dict_row) as cursor:
        session = _session_row(cursor, session_id, current_user.id, active_project=True)
        model = _resolve_model(cursor, payload.model_id)

        cursor.execute(
            """
            INSERT INTO playground_messages(session_id, role, content, model_id)
            VALUES (%s,'user',%s,%s)
            RETURNING id
            """,
            (session_id, content, model["id"]),
        )

        next_title = session["title"]
        if next_title == "New chat":
            cursor.execute(
                "SELECT count(*) FROM playground_messages WHERE session_id=%s AND role='user'",
                (session_id,),
            )
            if cursor.fetchone()["count"] == 1:
                next_title = _title_from_prompt(content)

        cursor.execute(
            """
            UPDATE playground_sessions
            SET title=%s, selected_model_id=%s, updated_at=now()
            WHERE id=%s
            """,
            (next_title, model["id"], session_id),
        )
        connection.commit()

        cursor.execute(
            """
            SELECT role, content
            FROM (
                SELECT role, content, created_at, id
                FROM playground_messages
                WHERE session_id=%s
                ORDER BY created_at DESC, id DESC
                LIMIT 40
            ) recent
            ORDER BY created_at, id
            """,
            (session_id,),
        )
        history = [{"role": row["role"], "content": row["content"]} for row in cursor.fetchall()]

    gateway_payload = {
        "user_id": current_user.id,
        "project_id": str(session["project_id"]),
        "session_id": str(session_id),
        "model": model["public_id"],
        "messages": history,
    }
    gateway_url = settings.gateway_internal_url.rstrip("/") + "/internal/playground/stream"
    internal_token = settings.playground_internal_token
    timeout_seconds = settings.playground_gateway_timeout_seconds
    model_id = model["id"]
    user_id = current_user.id

    async def generate():
        assistant_parts: list[str] = []
        summary: dict = {}
        event_name = ""

        try:
            timeout = httpx.Timeout(timeout_seconds)
            async with httpx.AsyncClient(timeout=timeout) as client:
                async with client.stream(
                    "POST",
                    gateway_url,
                    json=gateway_payload,
                    headers={
                        "X-Nexora-Internal-Token": internal_token,
                        "Accept": "text/event-stream",
                    },
                ) as response:
                    if response.status_code >= 400:
                        await response.aread()
                        yield _sse("error", {"message": _gateway_error(response), "status": response.status_code})
                        return

                    async for line in response.aiter_lines():
                        if line.startswith("event:"):
                            event_name = line[6:].strip()
                            continue
                        if not line.startswith("data:"):
                            if line == "":
                                event_name = ""
                            continue

                        data = line[5:].strip()
                        if event_name == "nexora_metrics":
                            try:
                                parsed = json.loads(data)
                            except json.JSONDecodeError:
                                yield _sse("error", {"message": "The model gateway returned invalid metrics."})
                                return
                            if isinstance(parsed, dict):
                                summary = parsed
                            event_name = ""
                            continue

                        if data == "[DONE]" or not data:
                            continue

                        try:
                            chunk = json.loads(data)
                        except json.JSONDecodeError:
                            yield _sse("error", {"message": "The model gateway returned an invalid stream."})
                            return

                        delta = ""
                        choices = chunk.get("choices") if isinstance(chunk, dict) else None
                        if isinstance(choices, list) and choices:
                            first = choices[0]
                            if isinstance(first, dict):
                                delta_payload = first.get("delta")
                                if isinstance(delta_payload, dict):
                                    value = delta_payload.get("content")
                                    if isinstance(value, str):
                                        delta = value

                        if delta:
                            assistant_parts.append(delta)
                            yield _sse("delta", {"content": delta})

        except asyncio.CancelledError:
            raise
        except httpx.TimeoutException:
            yield _sse("error", {"message": "The model request timed out.", "status": 504})
            return
        except httpx.HTTPError:
            yield _sse("error", {"message": "The model gateway is unavailable.", "status": 503})
            return

        metrics = summary.get("metrics") if isinstance(summary.get("metrics"), dict) else {}
        assistant_content = "".join(assistant_parts).strip()
        if not assistant_content:
            yield _sse("error", {"message": "The model returned an empty response."})
            return

        request_id_value = summary.get("request_id")
        request_id = request_id_value if isinstance(request_id_value, str) and request_id_value else None

        try:
            await asyncio.to_thread(
                _persist_stream_assistant,
                session_id=session_id,
                user_id=user_id,
                model_id=model_id,
                content=assistant_content,
                request_id=request_id,
                metrics=metrics,
            )
        except Exception:
            yield _sse("error", {"message": "The response completed but could not be saved to Playground history."})
            return

        yield _sse("done", {"request_id": request_id, "metrics": metrics})

    return StreamingResponse(
        generate(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",
        },
    )
