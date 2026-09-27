import hashlib
import hmac
import secrets

from argon2 import PasswordHasher
from argon2.exceptions import InvalidHashError, VerifyMismatchError


_password_hasher = PasswordHasher(
    time_cost=3,
    memory_cost=65536,
    parallelism=2,
    hash_len=32,
    salt_len=16,
)
_dummy_password_hash = _password_hasher.hash("nexora-dummy-password-verification-value")


def normalize_email(email: str) -> str:
    return email.strip().lower()


def hash_password(password: str) -> str:
    return _password_hasher.hash(password)


def verify_password(password_hash: str | None, password: str) -> bool:
    candidate = password_hash or _dummy_password_hash
    try:
        return _password_hasher.verify(candidate, password)
    except (VerifyMismatchError, InvalidHashError):
        return False


def password_needs_rehash(password_hash: str) -> bool:
    try:
        return _password_hasher.check_needs_rehash(password_hash)
    except InvalidHashError:
        return True


def generate_session_token(prefix: str) -> str:
    return f"{prefix}_{secrets.token_urlsafe(48)}"


def hash_session_token(raw_token: str, secret: str) -> bytes:
    if len(secret) < 32:
        raise RuntimeError("SESSION_SECRET must be at least 32 characters")
    return hmac.new(secret.encode("utf-8"), raw_token.encode("utf-8"), hashlib.sha256).digest()


def generate_csrf_token() -> str:
    return secrets.token_urlsafe(32)
