from uuid import uuid4

from fastapi.testclient import TestClient

from nexora_control import playground
from nexora_control.main import app


def unique_email(prefix: str) -> str:
    return f"{prefix}-{uuid4().hex}@example.com"


def register(client: TestClient, prefix: str = "playground") -> None:
    response = client.post(
        "/auth/register",
        json={
            "email": unique_email(prefix),
            "password": "correct-horse-battery",
            "display_name": "Playground Tester",
        },
    )
    assert response.status_code == 201, response.text


def csrf(client: TestClient) -> str:
    token = client.cookies.get("nexora_csrf")
    assert token
    return token


def project_id(client: TestClient) -> str:
    response = client.get("/projects")
    assert response.status_code == 200
    return response.json()[0]["id"]


def auto_model_id(client: TestClient) -> str:
    response = client.get("/catalog/models")
    assert response.status_code == 200
    auto = next(item for item in response.json() if item["public_id"] == "auto-free")
    return auto["id"]


def test_playground_session_history_and_chat_are_owner_scoped(monkeypatch) -> None:
    owner = TestClient(app)
    stranger = TestClient(app)
    register(owner, "owner")
    register(stranger, "stranger")

    pid = project_id(owner)
    model_id = auto_model_id(owner)

    created = owner.post(
        "/playground/sessions",
        json={"project_id": pid, "model_id": model_id},
        headers={"X-CSRF-Token": csrf(owner)},
    )
    assert created.status_code == 201, created.text
    session = created.json()
    session_id = session["id"]
    assert session["title"] == "New chat"
    assert session["selected_model_public_id"] == "auto-free"

    assert stranger.get(f"/playground/sessions/{session_id}").status_code == 404

    monkeypatch.setattr(
        playground,
        "_call_gateway",
        lambda *args, **kwargs: {
            "request_id": "req_playground_test_1",
            "model": "auto-free",
            "message": {
                "role": "assistant",
                "content": "Redis can coordinate concurrent work with atomic operations.",
            },
            "metrics": {
                "status": 200,
                "ttft_ms": 142,
                "latency_ms": 611,
                "prompt_tokens": 12,
                "completion_tokens": 18,
                "total_tokens": 30,
            },
        },
    )

    sent = owner.post(
        f"/playground/sessions/{session_id}/messages",
        json={"content": "Explain Redis concurrency", "model_id": model_id},
        headers={"X-CSRF-Token": csrf(owner)},
    )
    assert sent.status_code == 200, sent.text
    turn = sent.json()
    assert turn["user_message"]["role"] == "user"
    assert turn["assistant_message"]["role"] == "assistant"
    assert turn["assistant_message"]["status"] == 200
    assert turn["assistant_message"]["ttft_ms"] == 142
    assert turn["assistant_message"]["latency_ms"] == 611
    assert turn["assistant_message"]["prompt_tokens"] == 12
    assert turn["assistant_message"]["completion_tokens"] == 18
    assert turn["session"]["title"].startswith("Explain Redis concurrency")

    history = owner.get(f"/playground/sessions/{session_id}")
    assert history.status_code == 200, history.text
    messages = history.json()["messages"]
    assert [message["role"] for message in messages] == ["user", "assistant"]
    assert messages[1]["request_id"] == "req_playground_test_1"

    listed = owner.get(f"/playground/sessions?project_id={pid}")
    assert listed.status_code == 200
    assert listed.json()[0]["id"] == session_id
    assert "Redis" in listed.json()[0]["preview"]

    renamed = owner.patch(
        f"/playground/sessions/{session_id}",
        json={"title": "Concurrency test"},
        headers={"X-CSRF-Token": csrf(owner)},
    )
    assert renamed.status_code == 200
    assert renamed.json()["title"] == "Concurrency test"

    denied_delete = stranger.delete(
        f"/playground/sessions/{session_id}",
        headers={"X-CSRF-Token": csrf(stranger)},
    )
    assert denied_delete.status_code == 404

    deleted = owner.delete(
        f"/playground/sessions/{session_id}",
        headers={"X-CSRF-Token": csrf(owner)},
    )
    assert deleted.status_code == 204
    assert owner.get(f"/playground/sessions/{session_id}").status_code == 404


def test_playground_requires_csrf_and_active_project() -> None:
    client = TestClient(app)
    register(client)
    pid = project_id(client)
    model_id = auto_model_id(client)

    no_csrf = client.post(
        "/playground/sessions",
        json={"project_id": pid, "model_id": model_id},
    )
    assert no_csrf.status_code == 403

    archived = client.post(
        f"/projects/{pid}/archive",
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert archived.status_code == 200

    denied = client.post(
        "/playground/sessions",
        json={"project_id": pid, "model_id": model_id},
        headers={"X-CSRF-Token": csrf(client)},
    )
    assert denied.status_code == 404
