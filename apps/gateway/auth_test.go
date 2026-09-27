package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractBearerToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	r.Header.Set("Authorization", "Bearer nxa_test")
	if got := extractBearerToken(r); got != "nxa_test" {
		t.Fatalf("expected token, got %q", got)
	}
}

func TestExtractBearerTokenRejectsMalformedHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	r.Header.Set("Authorization", "nxa_test")
	if got := extractBearerToken(r); got != "" {
		t.Fatalf("expected empty token, got %q", got)
	}
}

func TestHashAPIKeyMatchesControlPlaneAlgorithm(t *testing.T) {
	got := hashAPIKey("nxa_test-secret", "pepper-a")
	const want = "3b14cc48b58d3f7f29d50e7606d8e47e8514c27558fcf2e952416ca11f47b4b5"
	if hex := fmtHex(got); hex != want {
		t.Fatalf("unexpected hash %s", hex)
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
