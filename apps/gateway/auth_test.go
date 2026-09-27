package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractBearerToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	r.Header.Set("Authorization", "Bearer nxa_live_0123456789ab.test")
	if got := extractBearerToken(r); got != "nxa_live_0123456789ab.test" {
		t.Fatalf("expected token, got %q", got)
	}
}

func TestExtractBearerTokenRejectsMalformedHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	r.Header.Set("Authorization", "nxa_live_0123456789ab.test")
	if got := extractBearerToken(r); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
}

func TestHashAPIKeyMatchesControlPlaneAlgorithm(t *testing.T) {
	got := hashAPIKey("nxa_live_0123456789ab.test-secret", "pepper-a")
	const want = "9f2810049b7ab6023ac293d54e19d594c33faf400657a19ed2a771a80df6844e"
	if hex := fmtHex(got); hex != want {
		t.Fatalf("unexpected hash %s", hex)
	}
}

func TestAPIKeyPrincipalAllowsAllFreeModels(t *testing.T) {
	principal := &APIKeyPrincipal{AllowAllFreeModels: true}
	if !principal.AllowsModel("auto-free") {
		t.Fatal("all-free key should allow auto-free")
	}
	if !principal.AllowsModel("nexora/example") {
		t.Fatal("all-free key should allow active free catalog models")
	}
}

func TestAPIKeyPrincipalEnforcesSelectedModels(t *testing.T) {
	principal := &APIKeyPrincipal{
		AllowAllFreeModels: false,
		AllowedModels: map[string]struct{}{
			"nexora/allowed": {},
		},
		DefaultModelID: sql.NullString{},
	}

	if !principal.AllowsModel("nexora/allowed") {
		t.Fatal("scoped key should allow its configured model")
	}
	if principal.AllowsModel("nexora/blocked") {
		t.Fatal("scoped key must reject models outside its scope")
	}
	if principal.AllowsModel("auto-free") {
		t.Fatal("scoped key must not implicitly gain auto-free access")
	}
}

func TestFilterModelsForPrincipal(t *testing.T) {
	models := []Model{
		{ID: "auto-free", Status: "active", Free: true},
		{ID: "nexora/allowed", Status: "active", Free: true},
		{ID: "nexora/blocked", Status: "active", Free: true},
	}
	principal := &APIKeyPrincipal{
		AllowedModels: map[string]struct{}{"nexora/allowed": {}},
	}

	filtered := filterModelsForPrincipal(models, principal)
	if len(filtered) != 1 || filtered[0].ID != "nexora/allowed" {
		t.Fatalf("unexpected filtered models: %#v", filtered)
	}
}

func fmtHex(value []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(value)*2)
	for i, b := range value {
		out[i*2] = digits[b>>4]
		out[i*2+1] = digits[b&0x0f]
	}
	return string(out)
}
