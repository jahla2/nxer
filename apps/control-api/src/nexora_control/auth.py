from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
import hmac

from fastapi import APIRouter, Depends, HTTPException, Request, Response, status
from pydantic import BaseModel, EmailStr, Field
from psycopg.errors import UniqueViolation
from psycopg.rows import dict_row

from nexora_control.auth_security import (
    generate_csrf_token,
    generate_session_token,
    hash_password,
    hash_session_token,
    normalize_email,
    password_needs_rehash,
    verify_password,
)
from nexora_control.config import Settings, get_settings
from nexora_control.database import get_connection


router = APIRouter(prefix="/auth", tags=["auth"])

ACCESS_COOKIE = "nexora_access"
REFRESH_COOKIE = "nexora_refresh"
CSRF_COOKIE = "nexora_csrf"


class UserView(BaseModel):
    id: str
    email: EmailStr
    display_name: str
    role: str
    email_verified: bool


class RegisterRequest(BaseModel):
    email: EmailStr
    password: str = Field(min_length=12, max_length=128)
    display_name: str = Field(min_length=2, max_length=120)


class LoginRequest(BaseModel):
    email: EmailStr
    password: str = Field(min_length=1, max_length=128)


class PasswordResetRequest(BaseModel):
    email: EmailStr


class PasswordResetRequested(BaseModel):
    message: str
    reset_token: str | None = None


class PasswordResetConfirm(BaseModel):
    token: str = Field(min_length=32, max_length=256)
    new_password: str = Field(min_length=12, max_length=128)


@dataclass(frozen=True)
class UserPrincipal:
    id: str
    email: str
    display_name: str
    role: str
    email_verified: bool
    session_id: str


def _user_view(row: dict) -> UserView:
    return UserView(
        id=str(row["id"]),
        email=row["email"],
        display_name=row["display_name"],
        role=row["role"],
        email_verified=row["email_verified"],
    )


def _set_auth_cookies(
    response: Response,
    access_token: str,
    refresh_token: str,
    csrf_token: str,
    settings: Settings,
) -> None:
    response.set_cookie(
        ACCESS_COOKIE,
        access_token,
        max_age=settings.access_token_ttl_minutes * 60,
        httponly=True,
        secure=settings.secure_cookies,
        samesite="lax",
        path="/",
    )
    response.set_cookie(
        REFRESH_COOKIE,
        refresh_token,
        max_age=settings.refresh_token_ttl_days * 86400,
        httponly=True,
        secure=settings.secure_cookies,
        samesite="lax",
        path="/",
    )
    response.set_cookie(
        CSRF_COOKIE,
        csrf_token,
        max_age=settings.refresh_token_ttl_days * 86400,
        httponly=False,
        secure=settings.secure_cookies,
        samesite="lax",
        path="/",
    )


def _clear_auth_cookies(response: Response, settings: Settings) -> None:
    for name, httponly in (
        (ACCESS_COOKIE, True),
        (REFRESH_COOKIE, True),
        (CSRF_COOKIE, False),
    ):
        response.delete_cookie(
            name,
            path="/",
            httponly=httponly,
            secure=settings.secure_cookies,
            samesite="lax",
        )


def _issue_session(connection, user_id: str, user_agent: str, settings: Settings) -> tuple[str, str, str]:
    access_token = generate_session_token("nxa_at")
    refresh_token = generate_session_token("nxa_rt")
    csrf_token = generate_csrf_token()
    now = datetime.now(timezone.utc)
    access_expires_at = now + timedelta(minutes=settings.access_token_ttl_minutes)
    refresh_expires_at = now + timedelta(days=settings.refresh_token_ttl_days)

    connection.execute(
        """
        INSERT INTO user_sessions (
            user_id, refresh_token_hash, access_token_hash,
            access_expires_at, expires_at, user_agent
        ) VALUES (%s,%s,%s,%s,%s,%s)
        """,
        (
            user_id,
            hash_session_token(refresh_token, settings.session_secret),
            hash_session_token(access_token, settings.session_secret),
            access_expires_at,
            refresh_expires_at,
            user_agent[:512],
        ),
    )
    return access_token, refresh_token, csrf_token


def require_csrf(request: Request) -> None:
    cookie = request.cookies.get(CSRF_COOKIE, "")
    header = request.headers.get("X-CSRF-Token", "")
    if not cookie or not header or not hmac.compare_digest(cookie, header):
        raise HTTPException(status_code=status.HTTP_403_FORBIDDEN, detail="Invalid CSRF token")


def get_current_user(
    request: Request,
    settings: Settings = Depends(get_settings),
) -> UserPrincipal:
    raw_access = request.cookies.get(ACCESS_COOKIE, "")
    if not raw_access:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Authentication required")

    token_hash = hash_session_token(raw_access, settings.session_secret)
    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                """
                SELECT
                    u.id, u.email, u.display_name, u.role, u.email_verified,
                    s.id AS session_id
                FROM user_sessions s
                JOIN users u ON u.id = s.user_id
                WHERE s.access_token_hash = %s
                  AND s.revoked_at IS NULL
                  AND s.access_expires_at > now()
                  AND s.expires_at > now()
                  AND u.status = 'active'
                LIMIT 1
                """,
                (token_hash,),
            )
            row = cursor.fetchone()

    if row is None:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Session expired or invalid")

    return UserPrincipal(
        id=str(row["id"]),
        email=row["email"],
        display_name=row["display_name"],
        role=row["role"],
        email_verified=row["email_verified"],
        session_id=str(row["session_id"]),
    )


@router.post("/register", response_model=UserView, status_code=status.HTTP_201_CREATED)
def register(
    payload: RegisterRequest,
    request: Request,
    response: Response,
    settings: Settings = Depends(get_settings),
) -> UserView:
    email = normalize_email(str(payload.email))
    display_name = payload.display_name.strip()
    password_hash = hash_password(payload.password)

    try:
        with get_connection() as connection:
            with connection.cursor(row_factory=dict_row) as cursor:
                cursor.execute(
                    """
                    INSERT INTO users (email, password_hash, display_name, status)
                    VALUES (%s,%s,%s,'active')
                    RETURNING id, email, display_name, role, email_verified
                    """,
                    (email, password_hash, display_name),
                )
                user = cursor.fetchone()
                cursor.execute(
                    "INSERT INTO projects (user_id, name, status) VALUES (%s, 'My Project', 'active')",
                    (user["id"],),
                )
                access, refresh, csrf = _issue_session(
                    connection,
                    str(user["id"]),
                    request.headers.get("user-agent", ""),
                    settings,
                )
            connection.commit()
    except UniqueViolation as exc:
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail="An account with this email already exists") from exc

    _set_auth_cookies(response, access, refresh, csrf, settings)
    return _user_view(user)


@router.post("/login", response_model=UserView)
def login(
    payload: LoginRequest,
    request: Request,
    response: Response,
    settings: Settings = Depends(get_settings),
) -> UserView:
    email = normalize_email(str(payload.email))
    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                """
                SELECT id, email, display_name, role, email_verified, password_hash
                FROM users
                WHERE lower(email) = %s AND status = 'active'
                LIMIT 1
                """,
                (email,),
            )
            user = cursor.fetchone()

            stored_hash = user["password_hash"] if user is not None else None
            if not verify_password(stored_hash, payload.password):
                raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Invalid email or password")

            if password_needs_rehash(stored_hash):
                cursor.execute(
                    "UPDATE users SET password_hash=%s, updated_at=now() WHERE id=%s",
                    (hash_password(payload.password), user["id"]),
                )

            access, refresh, csrf = _issue_session(
                connection,
                str(user["id"]),
                request.headers.get("user-agent", ""),
                settings,
            )
        connection.commit()

    _set_auth_cookies(response, access, refresh, csrf, settings)
    return _user_view(user)


@router.get("/me", response_model=UserView)
def me(current_user: UserPrincipal = Depends(get_current_user)) -> UserView:
    return UserView(
        id=current_user.id,
        email=current_user.email,
        display_name=current_user.display_name,
        role=current_user.role,
        email_verified=current_user.email_verified,
    )


@router.post("/refresh", response_model=UserView)
def refresh(
    request: Request,
    response: Response,
    settings: Settings = Depends(get_settings),
) -> UserView:
    raw_refresh = request.cookies.get(REFRESH_COOKIE, "")
    if not raw_refresh:
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Refresh session required")

    refresh_hash = hash_session_token(raw_refresh, settings.session_secret)
    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                """
                SELECT s.id AS session_id, s.user_id, u.id, u.email, u.display_name,
                       u.role, u.email_verified
                FROM user_sessions s
                JOIN users u ON u.id = s.user_id
                WHERE s.refresh_token_hash = %s
                  AND s.revoked_at IS NULL
                  AND s.expires_at > now()
                  AND u.status = 'active'
                FOR UPDATE OF s
                """,
                (refresh_hash,),
            )
            row = cursor.fetchone()
            if row is None:
                raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Refresh session expired or invalid")

            access_token = generate_session_token("nxa_at")
            refresh_token = generate_session_token("nxa_rt")
            csrf_token = generate_csrf_token()
            now = datetime.now(timezone.utc)
            cursor.execute(
                """
                UPDATE user_sessions
                SET access_token_hash=%s,
                    refresh_token_hash=%s,
                    access_expires_at=%s,
                    expires_at=%s,
                    updated_at=now()
                WHERE id=%s
                """,
                (
                    hash_session_token(access_token, settings.session_secret),
                    hash_session_token(refresh_token, settings.session_secret),
                    now + timedelta(minutes=settings.access_token_ttl_minutes),
                    now + timedelta(days=settings.refresh_token_ttl_days),
                    row["session_id"],
                ),
            )
        connection.commit()

    _set_auth_cookies(response, access_token, refresh_token, csrf_token, settings)
    return _user_view(row)


@router.post("/logout", status_code=status.HTTP_204_NO_CONTENT, dependencies=[Depends(require_csrf)])
def logout(
    request: Request,
    settings: Settings = Depends(get_settings),
) -> Response:
    raw_access = request.cookies.get(ACCESS_COOKIE, "")
    raw_refresh = request.cookies.get(REFRESH_COOKIE, "")

    with get_connection() as connection:
        if raw_refresh:
            connection.execute(
                "UPDATE user_sessions SET revoked_at=COALESCE(revoked_at, now()), updated_at=now() WHERE refresh_token_hash=%s",
                (hash_session_token(raw_refresh, settings.session_secret),),
            )
        elif raw_access:
            connection.execute(
                "UPDATE user_sessions SET revoked_at=COALESCE(revoked_at, now()), updated_at=now() WHERE access_token_hash=%s",
                (hash_session_token(raw_access, settings.session_secret),),
            )
        connection.commit()

    response = Response(status_code=status.HTTP_204_NO_CONTENT)
    _clear_auth_cookies(response, settings)
    return response


@router.post("/password-reset/request", response_model=PasswordResetRequested, status_code=status.HTTP_202_ACCEPTED)
def request_password_reset(
    payload: PasswordResetRequest,
    settings: Settings = Depends(get_settings),
) -> PasswordResetRequested:
    email = normalize_email(str(payload.email))
    raw_token: str | None = None

    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                "SELECT id FROM users WHERE lower(email)=%s AND status='active' LIMIT 1",
                (email,),
            )
            user = cursor.fetchone()
            if user is not None:
                raw_token = generate_session_token("nxa_pr")
                cursor.execute(
                    "UPDATE password_reset_tokens SET used_at=now() WHERE user_id=%s AND used_at IS NULL",
                    (user["id"],),
                )
                cursor.execute(
                    """
                    INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
                    VALUES (%s,%s,%s)
                    """,
                    (
                        user["id"],
                        hash_session_token(raw_token, settings.session_secret),
                        datetime.now(timezone.utc) + timedelta(minutes=settings.password_reset_ttl_minutes),
                    ),
                )
        connection.commit()

    return PasswordResetRequested(
        message="If the account exists, password reset instructions are available.",
        reset_token=raw_token if raw_token and settings.environment.lower() in {"development", "test", "local"} and settings.dev_expose_password_reset_token else None,
    )


@router.post("/password-reset/confirm", status_code=status.HTTP_204_NO_CONTENT)
def confirm_password_reset(
    payload: PasswordResetConfirm,
    settings: Settings = Depends(get_settings),
) -> Response:
    token_hash = hash_session_token(payload.token, settings.session_secret)
    with get_connection() as connection:
        with connection.cursor(row_factory=dict_row) as cursor:
            cursor.execute(
                """
                SELECT id, user_id
                FROM password_reset_tokens
                WHERE token_hash=%s
                  AND used_at IS NULL
                  AND expires_at > now()
                FOR UPDATE
                """,
                (token_hash,),
            )
            reset = cursor.fetchone()
            if reset is None:
                raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="Password reset token is invalid or expired")

            cursor.execute(
                "UPDATE users SET password_hash=%s, updated_at=now() WHERE id=%s",
                (hash_password(payload.new_password), reset["user_id"]),
            )
            cursor.execute(
                "UPDATE password_reset_tokens SET used_at=now() WHERE id=%s",
                (reset["id"],),
            )
            cursor.execute(
                "UPDATE user_sessions SET revoked_at=COALESCE(revoked_at, now()), updated_at=now() WHERE user_id=%s",
                (reset["user_id"],),
            )
        connection.commit()

    return Response(status_code=status.HTTP_204_NO_CONTENT)
