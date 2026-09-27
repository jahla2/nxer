package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeChatCompletionRequestAcceptsSupportedFields(t *testing.T) {
	body := `{"model":"auto-free","messages":[{"role":"user","content":"Hello"}],"temperature":0.7,"top_p":0.9,"max_tokens":128,"stream":true}`
	r:=httptest.NewRequest(http.MethodPost,"/v1/chat/completions",strings.NewReader(body))
	w:=httptest.NewRecorder()
	req,apiErr:=decodeChatCompletionRequest(w,r)
	if apiErr!=nil { t.Fatalf("unexpected error: %#v",apiErr) }
	if req.Model!="auto-free" || len(req.Messages)!=1 || !req.Stream { t.Fatalf("decoded request mismatch: %#v",req) }
}

func TestDecodeChatCompletionRequestRejectsUnknownParameter(t *testing.T) {
	body := `{"model":"auto-free","messages":[{"role":"user","content":"Hello"}],"provider":"openrouter"}`
	r:=httptest.NewRequest(http.MethodPost,"/v1/chat/completions",strings.NewReader(body))
	w:=httptest.NewRecorder()
	_,apiErr:=decodeChatCompletionRequest(w,r)
	if apiErr==nil || apiErr.Status!=http.StatusBadRequest { t.Fatalf("expected 400, got %#v",apiErr) }
}

func TestValidateChatCompletionRequestRejectsConflictingTokenLimits(t *testing.T) {
	a,b:=10,20
	req:=&ChatCompletionRequest{Model:"auto-free",Messages:[]ChatMessage{{Role:"user",Content:"Hello"}},MaxTokens:&a,MaxCompletionTokens:&b}
	apiErr:=validateChatCompletionRequest(req)
	if apiErr==nil || apiErr.Code!="NEXORA_CONFLICTING_TOKEN_LIMITS" { t.Fatalf("unexpected error: %#v",apiErr) }
}

func TestModelCatalogExposesAutoFree(t *testing.T) {
	models:=NewModelCatalog().ListActiveFree()
	if len(models)!=1 || models[0].ID!="auto-free" { t.Fatalf("expected auto-free, got %#v",models) }
}