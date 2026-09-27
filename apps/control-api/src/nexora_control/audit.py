from typing import Any

import psycopg
from psycopg.types.json import Jsonb


def write_audit(
    connection: psycopg.Connection,
    *,
    actor_user_id: str,
    action: str,
    resource_type: str,
    resource_id: str | None,
    metadata: dict[str, Any] | None = None,
) -> None:
    connection.execute(
        """
        INSERT INTO audit_logs (
            actor_user_id, action, resource_type, resource_id, metadata
        ) VALUES (%s,%s,%s,%s,%s)
        """,
        (
            actor_user_id,
            action,
            resource_type,
            resource_id,
            Jsonb(metadata or {}),
        ),
    )
