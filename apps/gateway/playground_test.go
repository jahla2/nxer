package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyInternalPlaygroundToken(t *testing.T) {
	t.Setenv("PLAYGROUND_INTERNAL_TOKEN", "test-playground-token-that-is-longer-than-thirty-two-characters")

	valid := httptest.NewRequest(http.MethodPost, "/internal/playground/chat", nil)
	valid.Header.Set("X-Nexora-Internal-Token", "test-playground-token-that-is-longer-than-thirty-two-characters")
	if !verifyInternalPlaygroundToken(valid) {
		t.Fatal("expected internal token to be accepted")
	}

	invalid := httptest.NewRequest(http.MethodPost, "/internal/playground/chat", nil)
	invalid.Header.Set("X-Nexora-Internal-Token", "wrong-token")
	if verifyInternalPlaygroundToken(invalid) {
		t.Fatal("expected invalid internal token to be rejected")
	}
}

func TestDecodePlaygroundChatRequestAcceptsConversation(t *testing.T) {
	body := `{
		"user_id":"11111111-1111-1111-1111-111111111111",
		"project_id":"22222222-2222-2222-2222-222222222222",
		"session_id":"33333333-3333-3333-3333-333333333333",
		"model":"auto-free",
		"messages":[
			{"role":"user","content":"Hello"},
			{"role":"assistant","content":"Hi"},
			{"role":"user","content":"Explain Redis"}
		]
	}`
	r := httptest.NewRequest(http.MethodPost, "/internal/playground/chat", strings.NewReader(body))
	w := httptest.NewRecorder()

	payload, apiErr := decodePlaygroundChatRequest(w, r)
	if apiErr != nil {
		t.Fatalf("unexpected error: %#v", apiErr)
	}
	if payload.Model != "auto-free" || len(payload.Messages) != 3 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestDecodePlaygroundChatRequestRejectsUnsupportedRoles(t *testing.T) {
	body := `{
		"user_id":"u1",
		"project_id":"p1",
		"session_id":"s1",
		"model":"auto-free",
		"messages":[{"role":"system","content":"hidden"}]
	}`
	r := httptest.NewRequest(http.MethodPost, "/internal/playground/chat", strings.NewReader(body))
	w := httptest.NewRecorder()

	_, apiErr := decodePlaygroundChatRequest(w, r)
	if apiErr == nil || apiErr.Code != "NEXORA_PLAYGROUND_ROLE_INVALID" {
		t.Fatalf("expected role validation error, got %#v", apiErr)
	}
}

func TestPlaygroundLimitErrorsAreSpecific(t *testing.T) {
	if got := playgroundLimitError(1); got.Status != http.StatusTooManyRequests || got.Code != "NEXORA_PLAYGROUND_RPM_LIMIT" {
		t.Fatalf("unexpected RPM error: %#v", got)
	}
	if got := playgroundLimitError(3); got.Code != "NEXORA_PLAYGROUND_CONCURRENCY_LIMIT" {
		t.Fatalf("unexpected concurrency error: %#v", got)
	}
}
