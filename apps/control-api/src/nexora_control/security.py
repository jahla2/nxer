import hashlib
import hmac
import secrets

API_KEY_PREFIX = "nxa_"


def generate_api_key() -> str:
    return API_KEY_PREFIX + secrets.token_urlsafe(32)


def key_prefix(raw_key: str) -> str:
    if not raw_key.startswith(API_KEY_PREFIX):
        raise ValueError("invalid Nexora API key prefix")
    return raw_key[:16]


def hash_api_key(raw_key: str, pepper: str) -> bytes:
    if not pepper:
        raise ValueError("API_KEY_HASH_PEPPER is required")
    return hmac.new(pepper.encode("utf-8"), raw_key.encode("utf-8"), hashlib.sha256).digest()


def verify_api_key(raw_key: str, expected_hash: bytes, pepper: str) -> bool:
    return hmac.compare_digest(hash_api_key(raw_key, pepper), expected_hash)
