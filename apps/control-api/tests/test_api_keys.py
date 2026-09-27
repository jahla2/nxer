from fastapi.testclient import TestClient

from nexora_control.config import get_settings
from nexora_control.main import app


def test_api_keys_require_control_auth(monkeypatch) -> None:
    monkeypatch.setenv("CONTROL_ADMIN_TOKEN", "test-admin-token")
    get_settings.cache_clear()
    client = TestClient(app)
    response = client.get("/api-keys?project_id=00000000-0000-0000-0000-000000000000")
    assert response.status_code == 401
    get_settings.cache_clear()


def test_api_keys_fail_closed_without_control_auth_configuration(monkeypatch) -> None:
    monkeypatch.delenv("CONTROL_ADMIN_TOKEN", raising=False)
    get_settings.cache_clear()
    client = TestClient(app)
    response = client.get("/api-keys?project_id=00000000-0000-0000-0000-000000000000")
    assert response.status_code == 503
    get_settings.cache_clear()
