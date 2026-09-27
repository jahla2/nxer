from uuid import uuid4

from fastapi.testclient import TestClient

from nexora_control.database import get_connection
from nexora_control.main import app


def unique_email(prefix: str) -> str:
    return f"{prefix}-{uuid4().hex}@example.com"


def register(client: TestClient, email: str, password: str = "correct-horse-battery") -> dict:
    response = client.post(
        "/auth/register",
        json={"email": email, "password": password, "display_name": "Test Developer"},
    )
    assert response.status_code == 201, response.text
    return response.json()


def csrf(client: TestClient) -> str:
    value = client.cookies.get("nexora_csrf")
    assert value
    return value


def test_register_session_csrf_project_logout_and_login() -> None:
    email = unique_email("session")
    password = "correct-horse-battery"
    client = TestClient(app)

    user = register(client, email, password)
    assert user["email"] == email

    me = client.get("/auth/me")
    assert me.status_code == 200
    assert me.json()["display_name"] == "Test Developer"

    projects = client.get("/projects")
    assert projects.status_code == 200
    assert any(project["name"] == "My Project" for project in projects.json())

    rejected = client.post("/projects", json={"name": "No CSRF"})
    assert rejected.status_code == 403

    created = client.post(
        "/projects",
        json={"name": "Owned Project"},
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert created.status_code == 201

    logged_out = client.post("/auth/logout", headers={"X-CSRF-Token": csrf(client)})
    assert logged_out.status_code == 204
    assert client.get("/auth/me").status_code == 401

    logged_in = client.post("/auth/login", json={"email": email, "password": password})
    assert logged_in.status_code == 200
    assert client.get("/auth/me").status_code == 200


def test_project_and_api_key_ownership_isolation() -> None:
    owner = TestClient(app)
    stranger = TestClient(app)
    register(owner, unique_email("owner"))
    register(stranger, unique_email("stranger"))

    owner_projects = owner.get("/projects").json()
    project_id = owner_projects[0]["id"]

    hidden = stranger.get(f"/api-keys?project_id={project_id}")
    assert hidden.status_code == 404

    created = owner.post(
        "/api-keys",
        json={"project_id": project_id, "name": "Owner key", "allow_all_free_models": True},
        headers={"X-CSRF-Token": csrf(owner)},
    )
    assert created.status_code == 201, created.text
    key_payload = created.json()
    assert key_payload["api_key"].startswith("nxa_live_")

    denied_revoke = stranger.post(
        f"/api-keys/{key_payload['id']}/revoke",
        headers={"X-CSRF-Token": csrf(stranger)},
    )
    assert denied_revoke.status_code == 404

    revoked = owner.post(
        f"/api-keys/{key_payload['id']}/revoke",
        headers={"X-CSRF-Token": csrf(owner)},
    )
    assert revoked.status_code == 200
    assert revoked.json()["status"] == "revoked"


def test_password_reset_revokes_existing_session_and_changes_password() -> None:
    client = TestClient(app)
    email = unique_email("reset")
    old_password = "correct-horse-battery"
    new_password = "new-correct-horse-battery"
    register(client, email, old_password)

    requested = client.post("/auth/password-reset/request", json={"email": email})
    assert requested.status_code == 202
    reset_token = requested.json()["reset_token"]
    assert reset_token

    confirmed = client.post(
        "/auth/password-reset/confirm",
        json={"token": reset_token, "new_password": new_password},
    )
    assert confirmed.status_code == 204

    assert client.get("/auth/me").status_code == 401
    assert client.post("/auth/login", json={"email": email, "password": old_password}).status_code == 401
    assert client.post("/auth/login", json={"email": email, "password": new_password}).status_code == 200



def test_email_verification_requires_csrf_and_updates_current_user() -> None:
    client = TestClient(app)
    email = unique_email("verify")
    user = register(client, email)
    assert user["email_verified"] is False

    rejected = client.post("/auth/email-verification/request")
    assert rejected.status_code == 403

    requested = client.post(
        "/auth/email-verification/request",
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert requested.status_code == 202, requested.text
    verification_token = requested.json()["verification_token"]
    assert verification_token
    assert verification_token.startswith("nxa_ev_")

    confirmed = client.post(
        "/auth/email-verification/confirm",
        json={"token": verification_token},
    )
    assert confirmed.status_code == 200, confirmed.text
    assert confirmed.json()["email"] == email
    assert confirmed.json()["email_verified"] is True

    me = client.get("/auth/me")
    assert me.status_code == 200
    assert me.json()["email_verified"] is True

    replay = client.post(
        "/auth/email-verification/confirm",
        json={"token": verification_token},
    )
    assert replay.status_code == 400


def test_registration_creates_hashed_email_verification_token() -> None:
    client = TestClient(app)
    email = unique_email("verification-storage")
    user = register(client, email)

    with get_connection() as connection:
        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT t.token_hash, t.expires_at, t.used_at
                FROM email_verification_tokens t
                JOIN users u ON u.id=t.user_id
                WHERE lower(u.email)=%s
                ORDER BY t.created_at DESC
                LIMIT 1
                """,
                (email,),
            )
            row = cursor.fetchone()

    assert row is not None
    token_hash, expires_at, used_at = row
    assert bytes(token_hash)
    assert b"nxa_ev_" not in bytes(token_hash)
    assert expires_at is not None
    assert used_at is None



def test_password_reset_request_cooldown_does_not_issue_multiple_tokens() -> None:
    client = TestClient(app)
    email = unique_email("reset-cooldown")
    register(client, email)

    first = client.post("/auth/password-reset/request", json={"email": email})
    assert first.status_code == 202
    assert first.json()["reset_token"]

    second = client.post("/auth/password-reset/request", json={"email": email})
    assert second.status_code == 202
    assert second.json()["message"] == first.json()["message"]
    assert second.json()["reset_token"] is None

    with get_connection() as connection:
        with connection.cursor() as cursor:
            cursor.execute(
                """
                SELECT count(*)
                FROM password_reset_tokens t
                JOIN users u ON u.id=t.user_id
                WHERE lower(u.email)=%s
                """,
                (email,),
            )
            count = cursor.fetchone()[0]

    assert count == 1
