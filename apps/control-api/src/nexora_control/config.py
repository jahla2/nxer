from functools import lru_cache

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    app_name: str = "Nexora Control API"
    environment: str = "development"

    database_url: str = "postgresql://nexora:nexora-local-change-me@postgres:5432/nexora"
    redis_url: str = "redis://:nexora-redis-local-change-me@redis:6379/0"

    api_key_hash_pepper: str = "local-dev-change-this-to-a-long-random-secret"
    session_secret: str = "local-dev-change-this-to-another-long-random-secret"
    control_admin_token: str = "local-dev-admin-token-change-me"

    access_token_ttl_minutes: int = Field(default=15, ge=1, le=60)
    refresh_token_ttl_days: int = Field(default=30, ge=1, le=90)
    password_reset_ttl_minutes: int = Field(default=30, ge=5, le=120)

    local_bootstrap_enabled: bool = True
    local_admin_email: str = "local@nexora.dev"
    local_admin_password: str = "change-me-local-only"
    local_admin_display_name: str = "Local Developer"
    dev_expose_password_reset_token: bool = True

    @property
    def secure_cookies(self) -> bool:
        return self.environment.lower() not in {"development", "test", "local"}


@lru_cache
def get_settings() -> Settings:
    return Settings()
