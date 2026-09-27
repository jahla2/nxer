from psycopg.rows import dict_row

from nexora_control.auth_security import hash_password, normalize_email
from nexora_control.config import get_settings
from nexora_control.database import get_connection


def main() -> None:
    settings = get_settings()
    if settings.environment.lower() not in {"development", "local"} or not settings.local_bootstrap_enabled:
        print("Local bootstrap skipped")
        return
    if len(settings.local_admin_password) < 12:
        raise RuntimeError("LOCAL_ADMIN_PASSWORD must be at least 12 characters")

    email = normalize_email(settings.local_admin_email)
    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                "SELECT id, password_hash FROM users WHERE lower(email)=%s LIMIT 1",
                (email,),
            )
            user = cursor.fetchone()
            if user is None:
                cursor.execute(
                    """
                    INSERT INTO users (
                        email, password_hash, display_name, role, email_verified, status
                    ) VALUES (%s,%s,%s,'admin',true,'active')
                    RETURNING id, password_hash
                    """,
                    (
                        email,
                        hash_password(settings.local_admin_password),
                        settings.local_admin_display_name.strip(),
                    ),
                )
                user = cursor.fetchone()
            elif user["password_hash"] == "local-development-only":
                cursor.execute(
                    """
                    UPDATE users
                    SET password_hash=%s,
                        display_name=%s,
                        role='admin',
                        email_verified=true,
                        updated_at=now()
                    WHERE id=%s
                    """,
                    (
                        hash_password(settings.local_admin_password),
                        settings.local_admin_display_name.strip(),
                        user["id"],
                    ),
                )

            cursor.execute(
                """
                INSERT INTO projects (user_id, name, status)
                SELECT %s, 'Local Project', 'active'
                WHERE NOT EXISTS (
                    SELECT 1 FROM projects WHERE user_id=%s AND name='Local Project'
                )
                """,
                (user["id"], user["id"]),
            )
        connection.commit()

    print("Local development account is ready")


if __name__ == "__main__":
    main()
