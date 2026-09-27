from fastapi.testclient import TestClient
from nexora_control.main import app

client = TestClient(app)

def test_health() -> None:
    response = client.get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok", "service": "control-api"}
