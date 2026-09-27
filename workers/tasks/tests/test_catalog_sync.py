from __future__ import annotations

from catalog_sync import DiscoveredModel, parse_free_models, public_model_alias


def test_public_model_alias_is_stable_and_provider_neutral() -> None:
    alias = public_model_alias(
        "OpenRouter: Free Test Model",
        "provider/private-model-v1",
    )

    assert alias.startswith("nexora/")
    assert "openrouter" not in alias
    assert "provider/private-model-v1" not in alias
    assert alias == public_model_alias(
        "OpenRouter: Free Test Model",
        "provider/private-model-v1",
    )


def test_parse_free_models_filters_paid_non_text_and_duplicates() -> None:
    payload = {
        "data": [
            {
                "id": "provider/free-text",
                "name": "Free Text",
                "context_length": 8192,
                "pricing": {"prompt": "0", "completion": "0.000"},
                "architecture": {"output_modalities": ["text"]},
            },
            {
                "id": "provider/paid",
                "name": "Paid",
                "pricing": {"prompt": "0.1", "completion": "0"},
                "architecture": {"output_modalities": ["text"]},
            },
            {
                "id": "provider/free-image",
                "name": "Image",
                "pricing": {"prompt": "0", "completion": "0"},
                "architecture": {"output_modalities": ["image"]},
            },
            {
                "id": "provider/free-text",
                "name": "Duplicate",
                "pricing": {"prompt": "0", "completion": "0"},
                "architecture": {"output_modalities": ["text"]},
            },
        ]
    }

    models = parse_free_models(payload)

    assert models == [
        DiscoveredModel(
            upstream_id="provider/free-text",
            display_name="Free Text",
            context_length=8192,
            capabilities={"text": True, "streaming": True},
        )
    ]
