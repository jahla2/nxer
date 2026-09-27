from uuid import UUID, uuid4

from fastapi.testclient import TestClient

from nexora_control.database import get_connection
from nexora_control.gateway_cache import api_key_cache_key, get_gateway_cache_client
from nexora_control.main import app


def unique_email(prefix: str) -> str:
    return f"{prefix}-{uuid4().hex}@example.com"


def register(client: TestClient) -> None:
    response = client.post(
        "/auth/register",
        json={
            "email": unique_email("lifecycle"),
            "password": "correct-horse-battery",
            "display_name": "Lifecycle Tester",
        },
    )
    assert response.status_code == 201, response.text


def csrf(client: TestClient) -> str:
    token = client.cookies.get("nexora_csrf")
    assert token
    return token


def project_id(client: TestClient) -> str:
    projects = client.get("/projects")
    assert projects.status_code == 200
    return projects.json()[0]["id"]


def create_key(client: TestClient, pid: str, name: str = "Primary key") -> dict:
    response = client.post(
        "/api-keys",
        json={"project_id": pid, "name": name, "allow_all_free_models": True},
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert response.status_code == 201, response.text
    return response.json()


def test_project_and_api_key_full_lifecycle_and_audit() -> None:
    client = TestClient(app)
    register(client)
    pid = project_id(client)

    renamed_project = client.patch(
        f"/projects/{pid}",
        json={"name": "Renamed Project"},
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert renamed_project.status_code == 200
    assert renamed_project.json()["name"] == "Renamed Project"

    original = create_key(client, pid)
    raw_original = original["api_key"]
    original_id = original["id"]
    cache = get_gateway_cache_client()
    original_cache_key = api_key_cache_key(original["key_prefix"])
    cache.set(original_cache_key, b"stale-auth-cache", ex=300)

    renamed_key = client.patch(
        f"/api-keys/{original_id}",
        json={
            "name": "Production SDK",
            "requests_per_minute": 7,
            "requests_per_day": 25,
            "max_concurrent": 3,
        },
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert renamed_key.status_code == 200, renamed_key.text
    assert renamed_key.json()["name"] == "Production SDK"
    assert renamed_key.json()["requests_per_minute"] == 7
    assert renamed_key.json()["requests_per_day"] == 25
    assert renamed_key.json()["max_concurrent"] == 3
    assert cache.exists(original_cache_key) == 0

    model_id = uuid4()
    with get_connection() as connection:
        connection.execute(
            """
            INSERT INTO models(id, public_id, upstream_id, display_name, active, is_free)
            VALUES (%s,%s,%s,%s,true,true)
            """,
            (
                model_id,
                f"test-model-{model_id}",
                f"provider/test-model-{model_id}",
                "Test Free Model",
            ),
        )
        connection.commit()

    scoped = client.patch(
        f"/api-keys/{original_id}",
        json={
            "allow_all_free_models": False,
            "default_model_id": str(model_id),
            "model_ids": [str(model_id)],
        },
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert scoped.status_code == 200, scoped.text
    assert scoped.json()["allow_all_free_models"] is False
    assert scoped.json()["default_model_id"] == str(model_id)
    assert scoped.json()["model_ids"] == [str(model_id)]

    cache.set(original_cache_key, b"stale-auth-cache", ex=300)
    rotated = client.post(
        f"/api-keys/{original_id}/rotate",
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert rotated.status_code == 201, rotated.text
    rotated_payload = rotated.json()
    assert rotated_payload["api_key"] != raw_original
    assert rotated_payload["rotated_from_id"] == original_id
    assert rotated_payload["name"] == "Production SDK"
    assert rotated_payload["model_ids"] == [str(model_id)]
    assert cache.exists(original_cache_key) == 0

    with get_connection() as connection:
        with connection.cursor() as cursor:
            cursor.execute(
                "SELECT status, key_prefix, key_hash FROM api_keys WHERE id=%s",
                (UUID(original_id),),
            )
            old_status, old_prefix, old_hash = cursor.fetchone()
            cursor.execute(
                "SELECT status, key_prefix, key_hash, rotated_from_id FROM api_keys WHERE id=%s",
                (UUID(rotated_payload["id"]),),
            )
            new_status, new_prefix, new_hash, rotated_from = cursor.fetchone()
            cursor.execute(
                "SELECT action FROM audit_logs WHERE resource_type IN ('project','api_key') ORDER BY created_at",
            )
            actions = {row[0] for row in cursor.fetchall()}

    assert old_status == "revoked"
    assert new_status == "active"
    assert rotated_from == UUID(original_id)
    assert old_prefix != new_prefix
    assert raw_original.encode() not in bytes(old_hash)
    assert rotated_payload["api_key"].encode() not in bytes(new_hash)
    assert {
        "project.renamed",
        "api_key.created",
        "api_key.updated",
        "api_key.rotated",
    }.issubset(actions)

    rotated_cache_key = api_key_cache_key(rotated_payload["key_prefix"])
    cache.set(rotated_cache_key, b"stale-auth-cache", ex=300)
    archived = client.post(
        f"/projects/{pid}/archive",
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert archived.status_code == 200
    assert archived.json()["status"] == "archived"
    assert cache.exists(rotated_cache_key) == 0

    listed = client.get(f"/api-keys?project_id={pid}")
    assert listed.status_code == 200
    assert all(item["status"] == "revoked" for item in listed.json())

    denied_create = client.post(
        "/api-keys",
        json={"project_id": pid, "name": "Should fail", "allow_all_free_models": True},
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert denied_create.status_code == 404


def test_project_and_key_mutations_are_owner_scoped() -> None:
    owner = TestClient(app)
    stranger = TestClient(app)
    register(owner)
    register(stranger)

    pid = project_id(owner)
    key = create_key(owner, pid)

    assert stranger.patch(
        f"/projects/{pid}",
        json={"name": "Hijacked"},
        headers={"X-CSRF-Token": csrf(stranger)},
    ).status_code == 404

    assert stranger.post(
        f"/projects/{pid}/archive",
        headers={"X-CSRF-Token": csrf(stranger)},
    ).status_code == 404

    assert stranger.patch(
        f"/api-keys/{key['id']}",
        json={"name": "Hijacked key"},
        headers={"X-CSRF-Token": csrf(stranger)},
    ).status_code == 404

    assert stranger.post(
        f"/api-keys/{key['id']}/rotate",
        headers={"X-CSRF-Token": csrf(stranger)},
    ).status_code == 404


def test_revoke_invalidates_gateway_authorization_cache() -> None:
    client = TestClient(app)
    register(client)
    pid = project_id(client)
    created = create_key(client, pid, name="Revocation cache test")

    cache = get_gateway_cache_client()
    cache_key = api_key_cache_key(created["key_prefix"])
    cache.set(cache_key, b"stale-auth-cache", ex=300)
    assert cache.exists(cache_key) == 1

    revoked = client.post(
        f"/api-keys/{created['id']}/revoke",
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert revoked.status_code == 200, revoked.text
    assert revoked.json()["status"] == "revoked"
    assert cache.exists(cache_key) == 0



def test_console_model_catalog_is_authenticated_and_provider_neutral() -> None:
    anonymous = TestClient(app)
    assert anonymous.get("/catalog/models").status_code == 401

    client = TestClient(app)
    register(client)

    safe_id = uuid4()
    legacy_id = uuid4()
    with get_connection() as connection:
        connection.execute(
            """
            INSERT INTO models(
                id, public_id, upstream_id, display_name,
                active, is_free, capabilities, context_length
            )
            VALUES
                (%s,%s,%s,%s,true,true,%s::jsonb,%s),
                (%s,%s,%s,%s,true,true,%s::jsonb,%s)
            """,
            (
                safe_id,
                f"nexora/policy-test-{safe_id.hex[:12]}",
                f"provider/private-{safe_id}",
                "Policy Test Model",
                '{"text": true, "streaming": true}',
                8192,
                legacy_id,
                f"legacy/provider-{legacy_id}",
                f"provider/legacy-{legacy_id}",
                "Legacy Provider Model",
                '{"text": true}',
                4096,
            ),
        )
        connection.commit()

    response = client.get("/catalog/models")
    assert response.status_code == 200, response.text
    items = response.json()

    safe = next(item for item in items if item["id"] == str(safe_id))
    assert safe["public_id"].startswith("nexora/")
    assert safe["display_name"] == "Policy Test Model"
    assert safe["context_length"] == 8192
    assert safe["capabilities"]["text"] is True
    assert "upstream_id" not in safe
    assert "provider_key" not in safe

    assert all(item["id"] != str(legacy_id) for item in items)


def test_api_key_create_supports_complete_policy_configuration() -> None:
    client = TestClient(app)
    register(client)
    pid = project_id(client)

    model_id = uuid4()
    with get_connection() as connection:
        connection.execute(
            """
            INSERT INTO models(id, public_id, upstream_id, display_name, active, is_free)
            VALUES (%s,%s,%s,%s,true,true)
            """,
            (
                model_id,
                f"nexora/create-policy-{model_id.hex[:12]}",
                f"provider/create-policy-{model_id}",
                "Create Policy Model",
            ),
        )
        connection.commit()

    created = client.post(
        "/api-keys",
        json={
            "project_id": pid,
            "name": "Scoped production key",
            "allow_all_free_models": False,
            "default_model_id": str(model_id),
            "model_ids": [str(model_id)],
            "requests_per_minute": 6,
            "requests_per_day": 60,
            "max_concurrent": 2,
            "expires_at": "2099-01-01T00:00:00Z",
        },
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert created.status_code == 201, created.text
    payload = created.json()
    assert payload["allow_all_free_models"] is False
    assert payload["default_model_id"] == str(model_id)
    assert payload["model_ids"] == [str(model_id)]
    assert payload["requests_per_minute"] == 6
    assert payload["requests_per_day"] == 60
    assert payload["max_concurrent"] == 2
    assert payload["expires_at"].startswith("2099-01-01T00:00:00")
