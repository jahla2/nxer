import pytest
from pydantic import ValidationError

from nexora_control.config import Settings


def production_settings(**overrides):
    values = {
        "environment": "production",
        "database_url": "postgresql://nexora:strong-password@postgres:5432/nexora",
        "redis_url": "redis://:strong-redis-password@redis:6379/0",
        "api_key_hash_pepper": "api-key-pepper-that-is-definitely-long-enough",
        "session_secret": "session-secret-that-is-definitely-long-enough",
        "control_admin_token": "admin-token-that-is-definitely-long-enough",
        "dashboard_url": "https://gateway.example.test",
        "smtp_host": "smtp.example.test",
        "smtp_from_email": "noreply@example.test",
        "local_bootstrap_enabled": False,
        "dev_expose_password_reset_token": False,
        "dev_expose_email_verification_token": False,
    }
    values.update(overrides)
    return Settings(_env_file=None, **values)


def test_secure_production_settings_are_accepted() -> None:
    settings = production_settings()
    assert settings.secure_cookies is True
    assert settings.smtp_configured is True


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("api_key_hash_pepper", "local-dev-change-this"),
        ("session_secret", "short"),
        ("control_admin_token", "change-me-admin-token"),
        ("dashboard_url", "http://gateway.example.test"),
        ("local_bootstrap_enabled", True),
        ("dev_expose_password_reset_token", True),
        ("dev_expose_email_verification_token", True),
        ("smtp_host", ""),
    ],
)
def test_unsafe_production_settings_fail_fast(field: str, value) -> None:
    with pytest.raises(ValidationError):
        production_settings(**{field: value})
