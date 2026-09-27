from fastapi import FastAPI

from nexora_control.api_keys import router as api_keys_router
from nexora_control.auth import router as auth_router
from nexora_control.config import get_settings
from nexora_control.console import router as console_router
from nexora_control.database import get_connection
from nexora_control.operations import router as operations_router


settings = get_settings()
app = FastAPI(title=settings.app_name, version="0.1.0")
app.include_router(auth_router)
app.include_router(api_keys_router)
app.include_router(console_router)
app.include_router(operations_router)


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok", "service": "control-api"}


@app.get("/ready")
def ready() -> dict[str, str]:
    with get_connection() as connection:
        connection.execute("SELECT 1")
    return {"status": "ok", "service": "control-api"}
