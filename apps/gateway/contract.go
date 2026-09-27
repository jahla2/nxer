package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxMessages = 128

type ChatMessage struct {
	Role string `json:"role"`
	Content any `json:"content"`
}

type ChatCompletionRequest struct {
	Model string `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP *float64 `json:"top_p,omitempty"`
	MaxTokens *int `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int `json:"max_completion_tokens,omitempty"`
	Stream bool `json:"stream,omitempty"`
	Stop json.RawMessage `json:"stop,omitempty"`
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
	Tools json.RawMessage `json:"tools,omitempty"`
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`
}

func decodeChatCompletionRequest(w http.ResponseWriter, r *http.Request) (*ChatCompletionRequest,*APIError) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(getenvInt("MAX_REQUEST_BYTES",1048576)))
	decoder:=json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req ChatCompletionRequest
	if err:=decoder.Decode(&req); err!=nil {
		var maxErr *http.MaxBytesError
		if errors.As(err,&maxErr) { return nil,newAPIError(http.StatusRequestEntityTooLarge,"invalid_request_error","NEXORA_PAYLOAD_TOO_LARGE","Request payload is too large.") }
		if errors.Is(err,io.EOF) { return nil,newAPIError(http.StatusBadRequest,"invalid_request_error","NEXORA_INVALID_REQUEST","Request body is required.") }
		return nil,newAPIError(http.StatusBadRequest,"invalid_request_error","NEXORA_INVALID_REQUEST","Request body is invalid or contains unsupported parameters.")
	}
	if decoder.Decode(&struct{}{}) != io.EOF { return nil,newAPIError(http.StatusBadRequest,"invalid_request_error","NEXORA_INVALID_REQUEST","Request body must contain a single JSON object.") }
	if apiErr:=validateChatCompletionRequest(&req); apiErr!=nil { return nil,apiErr }
	return &req,nil
}

func validateChatCompletionRequest(req *ChatCompletionRequest) *APIError {
	req.Model=strings.TrimSpace(req.Model)
	if req.Model=="" { return newAPIError(http.StatusUnprocessableEntity,"invalid_request_error","NEXORA_MODEL_REQUIRED","Model is required.") }
	if len(req.Messages)==0 { return newAPIError(http.StatusUnprocessableEntity,"invalid_request_error","NEXORA_MESSAGES_REQUIRED","At least one message is required.") }
	if len(req.Messages)>maxMessages { return newAPIError(http.StatusUnprocessableEntity,"invalid_request_error","NEXORA_TOO_MANY_MESSAGES",fmt.Sprintf("Messages cannot exceed %d items.",maxMessages)) }
	for i,m:=range req.Messages {
		switch m.Role { case "system","user","assistant","tool","developer": default: return newAPIError(http.StatusUnprocessableEntity,"invalid_request_error","NEXORA_INVALID_MESSAGE_ROLE",fmt.Sprintf("Message %d has an unsupported role.",i)) }
		if m.Content==nil { return newAPIError(http.StatusUnprocessableEntity,"invalid_request_error","NEXORA_INVALID_MESSAGE_CONTENT",fmt.Sprintf("Message %d content is required.",i)) }
	}
	if req.Temperature!=nil && (*req.Temperature<0 || *req.Temperature>2) { return newAPIError(http.StatusUnprocessableEntity,"invalid_request_error","NEXORA_INVALID_TEMPERATURE","temperature must be between 0 and 2.") }
	if req.TopP!=nil && (*req.TopP<0 || *req.TopP>1) { return newAPIError(http.StatusUnprocessableEntity,"invalid_request_error","NEXORA_INVALID_TOP_P","top_p must be between 0 and 1.") }
	maxOutput:=getenvInt("MAX_OUTPUT_TOKENS",4096)
	if req.MaxTokens!=nil && (*req.MaxTokens<1 || *req.MaxTokens>maxOutput) { return newAPIError(http.StatusUnprocessableEntity,"invalid_request_error","NEXORA_INVALID_MAX_TOKENS",fmt.Sprintf("max_tokens must be between 1 and %d.",maxOutput)) }
	if req.MaxCompletionTokens!=nil && (*req.MaxCompletionTokens<1 || *req.MaxCompletionTokens>maxOutput) { return newAPIError(http.StatusUnprocessableEntity,"invalid_request_error","NEXORA_INVALID_MAX_COMPLETION_TOKENS",fmt.Sprintf("max_completion_tokens must be between 1 and %d.",maxOutput)) }
	if req.MaxTokens!=nil && req.MaxCompletionTokens!=nil { return newAPIError(http.StatusUnprocessableEntity,"invalid_request_error","NEXORA_CONFLICTING_TOKEN_LIMITS","Use either max_tokens or max_completion_tokens, not both.") }
	return nil
}