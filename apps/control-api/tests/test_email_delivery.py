import pytest

from nexora_control.config import Settings
from nexora_control.email_delivery import (
    EmailDeliveryError,
    email_verification_url,
    password_reset_url,
    send_verification_email,
)


def settings(**overrides) -> Settings:
    values = {
        "environment": "test",
        "dashboard_url": "https://console.example.test",
        "session_secret": "test-session-secret-that-is-longer-than-thirty-two-characters",
        "smtp_host": "",
        "smtp_from_email": "",
    }
    values.update(overrides)
    return Settings(_env_file=None, **values)


def test_auth_action_urls_target_dashboard_without_provider_details() -> None:
    config = settings()
    verification = email_verification_url(config, "nxa_ev_token-value")
    reset = password_reset_url(config, "nxa_pr_token-value")

    assert verification == "https://console.example.test/?verify_email=nxa_ev_token-value"
    assert reset == "https://console.example.test/?reset_password=nxa_pr_token-value"


def test_email_delivery_requires_explicit_smtp_configuration() -> None:
    config = settings()

    with pytest.raises(EmailDeliveryError, match="SMTP delivery is not configured"):
        send_verification_email(config, "developer@example.com", "nxa_ev_test-token")
