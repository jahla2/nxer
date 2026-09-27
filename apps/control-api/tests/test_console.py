from fastapi.testclient import TestClient
from nexora_control.main import app

client=TestClient(app)

def test_console_requires_admin_token():
    for path in ("/projects","/usage","/requests","/settings"):
        response=client.get(path)
        assert response.status_code in (401,503)
