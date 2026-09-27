from email.message import EmailMessage
import smtplib
import ssl
from urllib.parse import urlencode

from nexora_control.config import Settings


class EmailDeliveryError(RuntimeError):
    pass


def _action_url(settings: Settings, parameter: str, token: str) -> str:
    base = settings.dashboard_url.strip().rstrip("/")
    query = urlencode({parameter: token})
    return f"{base}/?{query}"


def email_verification_url(settings: Settings, token: str) -> str:
    return _action_url(settings, "verify_email", token)


def password_reset_url(settings: Settings, token: str) -> str:
    return _action_url(settings, "reset_password", token)


def _send_email(
    *,
    settings: Settings,
    recipient: str,
    subject: str,
    text_body: str,
    html_body: str,
) -> None:
    if not settings.smtp_configured:
        raise EmailDeliveryError("SMTP delivery is not configured")

    message = EmailMessage()
    from_name = settings.smtp_from_name.strip() or "Nexora"
    message["From"] = f"{from_name} <{settings.smtp_from_email.strip()}>"
    message["To"] = recipient
    message["Subject"] = subject
    message.set_content(text_body)
    message.add_alternative(html_body, subtype="html")

    try:
        if settings.smtp_use_ssl:
            context = ssl.create_default_context()
            with smtplib.SMTP_SSL(
                settings.smtp_host,
                settings.smtp_port,
                timeout=settings.smtp_timeout_seconds,
                context=context,
            ) as smtp:
                if settings.smtp_username:
                    smtp.login(settings.smtp_username, settings.smtp_password)
                smtp.send_message(message)
            return

        with smtplib.SMTP(
            settings.smtp_host,
            settings.smtp_port,
            timeout=settings.smtp_timeout_seconds,
        ) as smtp:
            smtp.ehlo()
            if settings.smtp_starttls:
                context = ssl.create_default_context()
                smtp.starttls(context=context)
                smtp.ehlo()
            if settings.smtp_username:
                smtp.login(settings.smtp_username, settings.smtp_password)
            smtp.send_message(message)
    except (OSError, smtplib.SMTPException) as exc:
        raise EmailDeliveryError("SMTP delivery failed") from exc


def send_verification_email(settings: Settings, recipient: str, token: str) -> None:
    link = email_verification_url(settings, token)
    _send_email(
        settings=settings,
        recipient=recipient,
        subject="Verify your Nexora email",
        text_body=(
            "Verify your Nexora email address by opening this link:\n\n"
            f"{link}\n\n"
            f"This link expires in {settings.email_verification_ttl_hours} hours. "
            "If you did not create this account, you can ignore this email."
        ),
        html_body=(
            "<p>Verify your Nexora email address by opening the link below.</p>"
            f'<p><a href="{link}">Verify email</a></p>'
            f"<p>This link expires in {settings.email_verification_ttl_hours} hours.</p>"
            "<p>If you did not create this account, you can ignore this email.</p>"
        ),
    )


def send_password_reset_email(settings: Settings, recipient: str, token: str) -> None:
    link = password_reset_url(settings, token)
    _send_email(
        settings=settings,
        recipient=recipient,
        subject="Reset your Nexora password",
        text_body=(
            "Reset your Nexora password by opening this link:\n\n"
            f"{link}\n\n"
            f"This link expires in {settings.password_reset_ttl_minutes} minutes. "
            "If you did not request a password reset, you can ignore this email."
        ),
        html_body=(
            "<p>Reset your Nexora password by opening the link below.</p>"
            f'<p><a href="{link}">Reset password</a></p>'
            f"<p>This link expires in {settings.password_reset_ttl_minutes} minutes.</p>"
            "<p>If you did not request a password reset, you can ignore this email.</p>"
        ),
    )
