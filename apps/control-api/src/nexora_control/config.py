from functools import lru_cache
from pydantic_settings import BaseSettings, SettingsConfigDict

class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")
    app_name: str = "Nexora Control API"
    environment: str = "development"
    database_url: str = "postgresql://nexora:nexora@postgres:5432/nexora"
    redis_url: str = "redis://:nexora@redis:6379/0"

@lru_cache
def get_settings() -> Settings:
    return Settings()
