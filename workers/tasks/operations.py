from __future__ import annotations

from contextlib import AbstractContextManager
from dataclasses import dataclass, field
import logging
import re
import socket
import time
from typing import Any
from uuid import UUID

import psycopg
from psycopg.types.json import Jsonb


logger = logging.getLogger("nexora.worker.operations")

_URL_CREDENTIALS = re.compile(r"(?i)([a-z][a-z0-9+.-]*://)([^@\s/]+)@")
_BEARER = re.compile(r"(?i)(bearer\s+)[^\s,;]+")
_MAX_ERROR_LENGTH = 500


def sanitize_error(exc: BaseException) -> str:
    text = f"{type(exc).__name__}: {exc}".replace("\n", " ").strip()
    text = _URL_CREDENTIALS.sub(r"\1***@", text)
    text = _BEARER.sub(r"\1***", text)
    if len(text) > _MAX_ERROR_LENGTH:
        text = text[: _MAX_ERROR_LENGTH - 1] + "…"
    return text


def _start_run(
    database_url: str,
    *,
    job_name: str,
    task_id: str | None,
    worker_name: str | None,
) -> UUID | None:
    try:
        with psycopg.connect(database_url) as conn, conn.cursor() as cur:
            cur.execute(
                """
                INSERT INTO background_job_runs (
                    job_name, task_id, status, worker_name
                )
                VALUES (%s,%s,'running',%s)
                RETURNING id
                """,
                (job_name, task_id, worker_name),
            )
            row = cur.fetchone()
            conn.commit()
            return row[0] if row else None
    except psycopg.Error as exc:
        logger.warning("unable to record background job start: %s", sanitize_error(exc))
        return None


def _finish_run(
    database_url: str,
    run_id: UUID | None,
    *,
    status: str,
    duration_ms: int,
    result: dict[str, Any] | None = None,
    error_message: str | None = None,
) -> None:
    if run_id is None:
        return
    try:
        with psycopg.connect(database_url) as conn:
            conn.execute(
                """
                UPDATE background_job_runs
                SET status=%s,
                    completed_at=now(),
                    duration_ms=%s,
                    result=%s,
                    error_message=%s
                WHERE id=%s
                """,
                (
                    status,
                    max(0, duration_ms),
                    Jsonb(result or {}),
                    error_message,
                    run_id,
                ),
            )
            conn.commit()
    except psycopg.Error as exc:
        logger.warning("unable to record background job completion: %s", sanitize_error(exc))


@dataclass
class JobRun(AbstractContextManager["JobRun"]):
    database_url: str
    job_name: str
    task_id: str | None = None
    worker_name: str | None = None
    result: dict[str, Any] = field(default_factory=dict)
    _run_id: UUID | None = field(default=None, init=False)
    _started: float = field(default=0.0, init=False)

    def __enter__(self) -> "JobRun":
        self._started = time.monotonic()
        self._run_id = _start_run(
            self.database_url,
            job_name=self.job_name,
            task_id=self.task_id,
            worker_name=self.worker_name or socket.gethostname(),
        )
        return self

    def __exit__(self, exc_type, exc, traceback) -> bool:
        duration_ms = int((time.monotonic() - self._started) * 1000)
        if exc is None:
            _finish_run(
                self.database_url,
                self._run_id,
                status="succeeded",
                duration_ms=duration_ms,
                result=self.result,
            )
            return False

        _finish_run(
            self.database_url,
            self._run_id,
            status="failed",
            duration_ms=duration_ms,
            result=self.result,
            error_message=sanitize_error(exc),
        )
        return False
