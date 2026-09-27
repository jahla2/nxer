import pytest

from nexora_control.security import generate_api_key, hash_api_key, key_prefix, verify_api_key


def test_generated_api_key_has_nexora_prefix_and_unique_secret() -> None:
    first = generate_api_key()
    second = generate_api_key()
    assert first.startswith("nxa_live_")
    assert second.startswith("nxa_live_")
    assert first != second
    assert "." in first
    assert len(first.split(".", 1)[1]) >= 40


def test_hash_is_deterministic_and_peppered() -> None:
    raw = "nxa_test-secret"
    first = hash_api_key(raw, "pepper-a")
    second = hash_api_key(raw, "pepper-a")
    other = hash_api_key(raw, "pepper-b")
    assert first == second
    assert first != other
    assert verify_api_key(raw, first, "pepper-a")


def test_key_prefix_rejects_non_nexora_key() -> None:
    with pytest.raises(ValueError):
        key_prefix("sk-not-nexora")


def test_hash_requires_pepper() -> None:
    with pytest.raises(ValueError):
        hash_api_key("nxa_test-secret", "")
