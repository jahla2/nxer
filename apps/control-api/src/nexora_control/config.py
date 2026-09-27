from functools import lru_cache

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    app_name: str = "Nexora Control API"
    environment: str = "development"

    database_url: str = "postgresql://nexora:nexora-local-change-me@postgres:5432/nexora"
    redis_url: str = "redis://:nexora-redis-local-change-me@redis:6379/0"
    free_model_sync_interval_minutes: int = Field(default=10, ge=1, le=1440)

    api_key_hash_pepper: str = "local-dev-change-this-to-a-long-random-secret"
    session_secret: str = "local-dev-change-this-to-another-long-random-secret"
    control_admin_token: str = "local-dev-admin-token-change-me"

    access_token_ttl_minutes: int = Field(default=15, ge=1, le=60)
    refresh_token_ttl_days: int = Field(default=30, ge=1, le=90)
    password_reset_ttl_minutes: int = Field(default=30, ge=5, le=120)
    password_reset_request_cooldown_seconds: int = Field(default=60, ge=30, le=600)
    email_verification_ttl_hours: int = Field(default=24, ge=1, le=168)
    dashboard_url: str = "http://localhost:8080"

    smtp_host: str = ""
    smtp_port: int = Field(default=587, ge=1, le=65535)
    smtp_username: str = ""
    smtp_password: str = ""
    smtp_from_email: str = ""
    smtp_from_name: str = "Nexora"
    smtp_starttls: bool = True
    smtp_use_ssl: bool = False
    smtp_timeout_seconds: int = Field(default=10, ge=1, le=60)

    local_bootstrap_enabled: bool = True
    local_admin_email: str = "local@nexora.dev"
    local_admin_password: str = "change-me-local-only"
    local_admin_display_name: str = "Local Developer"
    dev_expose_password_reset_token: bool = True
    dev_expose_email_verification_token: bool = True

    @property
    def smtp_configured(self) -> bool:
        return bool(self.smtp_host.strip() and self.smtp_from_email.strip())

    @property
    def secure_cookies(self) -> bool:
        return self.environment.lower() not in {"development", "test", "local"}


@lru_cache
def get_settings() -> Settings:
    return Settings()
