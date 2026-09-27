from uuid import uuid4

from fastapi.testclient import TestClient
from psycopg.types.json import Jsonb

from nexora_control.database import get_connection
from nexora_control.main import app


def _register(client: TestClient, prefix: str) -> dict:
    response = client.post(
        "/auth/register",
        json={
            "email": f"{prefix}-{uuid4().hex}@example.com",
            "password": "correct-horse-battery",
            "display_name": "Operations Tester",
        },
    )
    assert response.status_code == 201, response.text
    return response.json()


def test_operations_status_is_authenticated_and_reports_catalog_and_jobs() -> None:
    anonymous = TestClient(app)
    assert anonymous.get("/operations/status").status_code == 401

    client = TestClient(app)
    _register(client, "ops-status")

    with get_connection() as connection:
        connection.execute(
            """
            UPDATE catalog_sync_state
            SET status='succeeded',
                last_attempt_at=now(),
                last_success_at=now(),
                model_count=1,
                last_error=NULL,
                updated_at=now()
            WHERE provider_key='openrouter'
            """
        )
        connection.execute(
            """
            INSERT INTO background_job_runs(
                job_name,task_id,status,started_at,completed_at,duration_ms,result
            )
            VALUES (
                'nexora.aggregate_usage',
                %s,
                'succeeded',
                now(),
                now(),
                12,
                %s
            )
            """,
            (f"task-{uuid4().hex}", Jsonb({"events": 4})),
        )
        connection.commit()

    response = client.get("/operations/status")
    assert response.status_code == 200, response.text
    payload = response.json()

    assert payload["catalog"]["status"] == "succeeded"
    assert payload["catalog"]["stale"] is False
    assert payload["catalog"]["active_model_count"] >= 1
    assert any(
        job["job_name"] == "nexora.aggregate_usage"
        and job["status"] == "succeeded"
        for job in payload["jobs"]
    )


def test_admin_operations_endpoints_require_admin_role() -> None:
    client = TestClient(app)
    user = _register(client, "ops-admin")

    assert client.get("/admin/jobs").status_code == 403
    assert client.get("/admin/audit").status_code == 403

    with get_connection() as connection:
        connection.execute(
            "UPDATE users SET role='admin', updated_at=now() WHERE id=%s",
            (user["id"],),
        )
        connection.execute(
            """
            INSERT INTO background_job_runs(
                job_name,task_id,status,started_at,completed_at,duration_ms,result
            )
            VALUES ('nexora.cleanup_housekeeping',%s,'succeeded',now(),now(),8,%s)
            """,
            (f"task-{uuid4().hex}", Jsonb({"deleted_usage_events": 1})),
        )
        connection.execute(
            """
            INSERT INTO audit_logs(
                actor_user_id,action,resource_type,resource_id,metadata
            )
            VALUES (%s,'operations.test','test_resource','resource-1',%s)
            """,
            (user["id"], Jsonb({"safe": True})),
        )
        connection.commit()

    jobs = client.get("/admin/jobs?limit=10")
    audit = client.get("/admin/audit?limit=10")

    assert jobs.status_code == 200, jobs.text
    assert audit.status_code == 200, audit.text
    assert any(item["job_name"] == "nexora.cleanup_housekeeping" for item in jobs.json())
    assert any(item["action"] == "operations.test" for item in audit.json())
