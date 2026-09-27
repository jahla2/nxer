from fastapi import FastAPI
from nexora_control.config import get_settings

settings = get_settings()
app = FastAPI(title=settings.app_name, version="0.1.0")

@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok", "service": "control-api"}

@app.get("/ready")
def ready() -> dict[str, str]:
    return {"status": "ok", "service": "control-api"}
