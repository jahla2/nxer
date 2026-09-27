package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicModelAliasIsStableAndProviderNeutral(t *testing.T) {
	first := publicModelAlias("Free Text Model", "vendor/free-text:free")
	second := publicModelAlias("Free Text Model", "vendor/free-text:free")

	if first != second {
		t.Fatalf("alias must be deterministic: %q != %q", first, second)
	}
	if !strings.HasPrefix(first, "nexora/free-text-model-") {
		t.Fatalf("unexpected alias %q", first)
	}
	if strings.Contains(strings.ToLower(first), "vendor") ||
		strings.Contains(strings.ToLower(first), "openrouter") {
		t.Fatalf("provider identity leaked in public alias %q", first)
	}
}

func TestModelSerializationNeverExposesInternalRoutingMetadata(t *testing.T) {
	model := Model{
		ID:            "nexora/free-text-abcdef123456",
		Object:        "model",
		OwnedBy:       "nexora",
		DisplayName:   "Free Text",
		ContextLength: 8192,
		Capabilities:  map[string]bool{"text": true},
		Status:        "active",
		Free:          true,
		UpstreamID:    "vendor/private-route",
		ProviderKey:   "openrouter",
	}

	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)

	if strings.Contains(body, "vendor/private-route") ||
		strings.Contains(strings.ToLower(body), "openrouter") ||
		strings.Contains(body, "UpstreamID") ||
		strings.Contains(body, "ProviderKey") {
		t.Fatalf("internal routing metadata leaked: %s", body)
	}
	if !strings.Contains(body, `"owned_by":"nexora"`) {
		t.Fatalf("expected Nexora public ownership: %s", body)
	}
}

func TestCatalogNormalizesPublicOwnershipAndOrdering(t *testing.T) {
	catalog := NewModelCatalog()
	catalog.Replace([]Model{
		{
			ID:          "nexora/z-model-111111111111",
			OwnedBy:     "upstream-provider",
			Status:      "active",
			Free:        true,
			UpstreamID:  "vendor/z",
			ProviderKey: openRouterProviderKey,
		},
		{
			ID:          autoFreeModelID,
			OwnedBy:     "upstream-provider",
			Status:      "active",
			Free:        true,
			UpstreamID:  autoFreeUpstreamID,
			ProviderKey: openRouterProviderKey,
		},
	})

	models := catalog.ListActiveFree()
	if len(models) != 2 {
		t.Fatalf("expected two models, got %#v", models)
	}
	if models[0].ID != autoFreeModelID {
		t.Fatalf("auto-free should be first, got %#v", models)
	}
	for _, model := range models {
		if model.OwnedBy != nexoraOwnedBy {
			t.Fatalf("public ownership was not normalized: %#v", model)
		}
	}
}
