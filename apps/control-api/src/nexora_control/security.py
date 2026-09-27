import hashlib
import hmac
import secrets

API_KEY_PREFIX = "nxa_live_"


def generate_api_key() -> str:
    public_prefix = secrets.token_hex(6)
    secret = secrets.token_urlsafe(48)
    return f"{API_KEY_PREFIX}{public_prefix}.{secret}"


def key_prefix(raw_key: str) -> str:
    if not raw_key.startswith(API_KEY_PREFIX):
        raise ValueError("invalid Nexora API key prefix")
    return raw_key.split(".", 1)[0]


def hash_api_key(raw_key: str, pepper: str) -> bytes:
    if not pepper:
        raise ValueError("API_KEY_HASH_PEPPER is required")
    return hmac.new(pepper.encode("utf-8"), raw_key.encode("utf-8"), hashlib.sha256).digest()


def verify_api_key(raw_key: str, expected_hash: bytes, pepper: str) -> bool:
    return hmac.compare_digest(hash_api_key(raw_key, pepper), expected_hash)
