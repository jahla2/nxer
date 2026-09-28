package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const maxPlaygroundMessages = 40

type PlaygroundChatRequest struct {
	UserID    string        `json:"user_id"`
	ProjectID string        `json:"project_id"`
	SessionID string        `json:"session_id"`
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
}

type PlaygroundMetrics struct {
	Status           int  `json:"status"`
	TTFTMS           *int `json:"ttft_ms,omitempty"`
	LatencyMS        *int `json:"latency_ms,omitempty"`
	PromptTokens     *int `json:"prompt_tokens,omitempty"`
	CompletionTokens *int `json:"completion_tokens,omitempty"`
	TotalTokens      *int `json:"total_tokens,omitempty"`
}

type PlaygroundChatResponse struct {
	RequestID string            `json:"request_id"`
	Model     string            `json:"model"`
	Message   map[string]string `json:"message"`
	Metrics   PlaygroundMetrics `json:"metrics"`
}

type PlaygroundLease struct {
	controller *AdmissionController
	userID     string
	projectID  string
	token      string
	released   bool
}

var playgroundAdmissionScript = redis.NewScript(`
local user_rpm = tonumber(ARGV[1])
local user_daily = tonumber(ARGV[2])
local user_concurrent = tonumber(ARGV[3])
local project_concurrent = tonumber(ARGV[4])
local global_rpm = tonumber(ARGV[5])
local global_daily = tonumber(ARGV[6])
local global_concurrent = tonumber(ARGV[7])
local minute_ttl = tonumber(ARGV[8])
local day_ttl = tonumber(ARGV[9])
local concurrency_ttl = tonumber(ARGV[10])
local now_ms = tonumber(ARGV[11])
local expiry_ms = tonumber(ARGV[12])
local lease_token = ARGV[13]

local function current_counter(key)
  local v = redis.call('GET', key)
  if not v then return 0 end
  return tonumber(v)
end

local function clean_concurrency(key)
  redis.call('ZREMRANGEBYSCORE', key, '-inf', now_ms)
end

clean_concurrency(KEYS[3])
clean_concurrency(KEYS[4])
clean_concurrency(KEYS[7])

if current_counter(KEYS[1]) >= user_rpm then return {0, 1} end
if current_counter(KEYS[2]) >= user_daily then return {0, 2} end
if redis.call('ZCARD', KEYS[3]) >= user_concurrent then return {0, 3} end
if redis.call('ZCARD', KEYS[4]) >= project_concurrent then return {0, 4} end
if current_counter(KEYS[5]) >= global_rpm then return {0, 5} end
if current_counter(KEYS[6]) >= global_daily then return {0, 6} end
if redis.call('ZCARD', KEYS[7]) >= global_concurrent then return {0, 7} end

local function increment_with_ttl(key, ttl)
  local v = redis.call('INCR', key)
  if v == 1 then redis.call('EXPIRE', key, ttl) end
end

local function add_lease(key)
  redis.call('ZADD', key, expiry_ms, lease_token)
  redis.call('EXPIRE', key, concurrency_ttl + 60)
end

increment_with_ttl(KEYS[1], minute_ttl)
increment_with_ttl(KEYS[2], day_ttl)
increment_with_ttl(KEYS[5], minute_ttl)
increment_with_ttl(KEYS[6], day_ttl)
add_lease(KEYS[3])
add_lease(KEYS[4])
add_lease(KEYS[7])
return {1, 0}
`)

func verifyInternalPlaygroundToken(r *http.Request) bool {
	expected := strings.TrimSpace(getenv("PLAYGROUND_INTERNAL_TOKEN", ""))
	provided := strings.TrimSpace(r.Header.Get("X-Nexora-Internal-Token"))
	if expected == "" || provided == "" {
		return false
	}
	expectedHash := sha256.Sum256([]byte(expected))
	providedHash := sha256.Sum256([]byte(provided))
	return hmac.Equal(expectedHash[:], providedHash[:])
}

func decodePlaygroundChatRequest(w http.ResponseWriter, r *http.Request) (*PlaygroundChatRequest, *APIError) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(getenvInt("MAX_REQUEST_BYTES", 1048576)))
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var payload PlaygroundChatRequest
	if err := decoder.Decode(&payload); err != nil {
		return nil, newAPIError(http.StatusBadRequest, "invalid_request_error", "NEXORA_INVALID_PLAYGROUND_REQUEST", "Playground request body is invalid.")
	}
	if strings.TrimSpace(payload.UserID) == "" || strings.TrimSpace(payload.ProjectID) == "" || strings.TrimSpace(payload.SessionID) == "" {
		return nil, newAPIError(http.StatusUnprocessableEntity, "invalid_request_error", "NEXORA_PLAYGROUND_CONTEXT_REQUIRED", "Playground user, project and session context are required.")
	}
	if len(payload.Messages) == 0 || len(payload.Messages) > maxPlaygroundMessages {
		return nil, newAPIError(http.StatusUnprocessableEntity, "invalid_request_error", "NEXORA_PLAYGROUND_MESSAGES_INVALID", "Playground conversation history is invalid.")
	}

	req := &ChatCompletionRequest{
		Model:    payload.Model,
		Messages: payload.Messages,
		Stream:   false,
	}
	if apiErr := validateChatCompletionRequest(req); apiErr != nil {
		return nil, apiErr
	}
	for _, message := range req.Messages {
		if message.Role != "user" && message.Role != "assistant" {
			return nil, newAPIError(http.StatusUnprocessableEntity, "invalid_request_error", "NEXORA_PLAYGROUND_ROLE_INVALID", "Playground history only supports user and assistant messages.")
		}
		if _, ok := message.Content.(string); !ok {
			return nil, newAPIError(http.StatusUnprocessableEntity, "invalid_request_error", "NEXORA_PLAYGROUND_CONTENT_INVALID", "Playground messages must contain text.")
		}
	}
	return &payload, nil
}

func (a *AdmissionController) AdmitPlayground(ctx context.Context, userID, projectID string) (*PlaygroundLease, *APIError) {
	if a == nil || a.client == nil {
		return nil, newAPIError(http.StatusServiceUnavailable, "service_unavailable", "NEXORA_ADMISSION_UNAVAILABLE", "Request admission service is unavailable.")
	}

	token, err := playgroundLeaseToken()
	if err != nil {
		return nil, newAPIError(http.StatusServiceUnavailable, "service_unavailable", "NEXORA_ADMISSION_UNAVAILABLE", "Unable to create request lease.")
	}

	now := time.Now().UTC()
	minute := now.Format("200601021504")
	day := now.Format("20060102")
	concurrencyTTL := getenvInt("CONCURRENCY_LEASE_TTL_SECONDS", 300)

	keys := []string{
		"nxa:playground:rate:user:" + userID + ":" + minute,
		"nxa:playground:daily:user:" + userID + ":" + day,
		"nxa:playground:concurrent:user:" + userID,
		"nxa:concurrent:v2:project:" + projectID,
		"nxa:rate:global:" + minute,
		"nxa:daily:global:" + day,
		"nxa:concurrent:v2:global",
	}
	nowMS := now.UnixMilli()
	expiryMS := now.Add(time.Duration(concurrencyTTL) * time.Second).UnixMilli()
	args := []any{
		getenvInt("PLAYGROUND_RPM_LIMIT", 10),
		getenvInt("PLAYGROUND_DAILY_LIMIT", 100),
		getenvInt("PLAYGROUND_MAX_CONCURRENT", 2),
		getenvInt("DEFAULT_PROJECT_MAX_CONCURRENT", 4),
		getenvInt("GLOBAL_RPM_LIMIT", 18),
		getenvInt("GLOBAL_DAILY_LIMIT", 45),
		getenvInt("GLOBAL_MAX_CONCURRENT", 8),
		120,
		172800,
		concurrencyTTL,
		nowMS,
		expiryMS,
		token,
	}

	result, err := playgroundAdmissionScript.Run(ctx, a.client, keys, args...).Slice()
	if err != nil || len(result) != 2 {
		return nil, newAPIError(http.StatusServiceUnavailable, "service_unavailable", "NEXORA_ADMISSION_UNAVAILABLE", "Request admission service is unavailable.")
	}
	allowed, _ := result[0].(int64)
	if allowed != 1 {
		code, _ := result[1].(int64)
		return nil, playgroundLimitError(code)
	}

	return &PlaygroundLease{
		controller: a,
		userID:     userID,
		projectID:  projectID,
		token:      token,
	}, nil
}

func (l *PlaygroundLease) Release(ctx context.Context) {
	if l == nil || l.released || l.controller == nil {
		return
	}
	l.released = true
	keys := []string{
		"nxa:playground:concurrent:user:" + l.userID,
		"nxa:concurrent:v2:project:" + l.projectID,
		"nxa:concurrent:v2:global",
	}
	_, _ = releaseScript.Run(ctx, l.controller.client, keys, l.token).Result()
}

func playgroundLeaseToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func playgroundLimitError(code int64) *APIError {
	switch code {
	case 1:
		return newAPIError(http.StatusTooManyRequests, "rate_limit_error", "NEXORA_PLAYGROUND_RPM_LIMIT", "Playground requests-per-minute limit exceeded.")
	case 2:
		return newAPIError(http.StatusTooManyRequests, "rate_limit_error", "NEXORA_PLAYGROUND_DAILY_LIMIT", "Playground daily request limit exceeded.")
	case 3:
		return newAPIError(http.StatusTooManyRequests, "rate_limit_error", "NEXORA_PLAYGROUND_CONCURRENCY_LIMIT", "Playground concurrent request limit exceeded.")
	case 4:
		return newAPIError(http.StatusTooManyRequests, "rate_limit_error", "NEXORA_PROJECT_CONCURRENCY_LIMIT", "Project concurrent request limit exceeded.")
	case 5:
		return newAPIError(http.StatusTooManyRequests, "rate_limit_error", "NEXORA_GLOBAL_RPM_LIMIT", "Gateway requests-per-minute safety limit exceeded.")
	case 6:
		return newAPIError(http.StatusTooManyRequests, "rate_limit_error", "NEXORA_GLOBAL_DAILY_LIMIT", "Gateway daily safety limit exceeded.")
	case 7:
		return newAPIError(http.StatusTooManyRequests, "rate_limit_error", "NEXORA_GLOBAL_CONCURRENCY_LIMIT", "Gateway concurrent request safety limit exceeded.")
	default:
		return newAPIError(http.StatusTooManyRequests, "rate_limit_error", "NEXORA_RATE_LIMIT", "Request limit exceeded.")
	}
}

func playgroundInternalHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRequestAPIError(w, r, newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "NEXORA_METHOD_NOT_ALLOWED", "Method not allowed."))
		return
	}
	if !verifyInternalPlaygroundToken(r) {
		writeRequestAPIError(w, r, newAPIError(http.StatusUnauthorized, "authentication_error", "NEXORA_INTERNAL_AUTH_FAILED", "Internal playground authentication failed."))
		return
	}

	payload, apiErr := decodePlaygroundChatRequest(w, r)
	if apiErr != nil {
		writeRequestAPIError(w, r, apiErr)
		return
	}

	route, ok := modelCatalog.Get(strings.TrimSpace(payload.Model))
	if !ok {
		writeRequestAPIError(w, r, newAPIError(http.StatusServiceUnavailable, "service_unavailable", "NEXORA_MODEL_UNAVAILABLE", "Requested model is not available."))
		return
	}

	lease, admissionErr := admissionController.AdmitPlayground(r.Context(), payload.UserID, payload.ProjectID)
	if admissionErr != nil {
		writeRequestAPIError(w, r, admissionErr)
		return
	}
	defer lease.Release(context.WithoutCancel(r.Context()))

	req := &ChatCompletionRequest{
		Model:    strings.TrimSpace(payload.Model),
		Messages: payload.Messages,
		Stream:   false,
	}
	buffered := NewBufferedResponseWriter()
	metrics, providerErr := inferenceProvider.Chat(r.Context(), req, route, buffered)
	if providerErr != nil {
		writeRequestAPIError(w, r, providerErr)
		return
	}
	if metrics == nil || !metrics.Completed {
		writeRequestAPIError(w, r, newAPIError(http.StatusBadGateway, "upstream_error", "NEXORA_UPSTREAM_ERROR", "Upstream provider response did not complete."))
		return
	}

	var completion map[string]any
	if err := json.Unmarshal(buffered.Body(), &completion); err != nil {
		writeRequestAPIError(w, r, newAPIError(http.StatusBadGateway, "upstream_error", "NEXORA_UPSTREAM_INVALID_RESPONSE", "Upstream provider returned an invalid response."))
		return
	}

	content := ""
	finishReason := ""
	if choices, ok := completion["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if reason, ok := choice["finish_reason"].(string); ok {
				finishReason = reason
			}
			if message, ok := choice["message"].(map[string]any); ok {
				if value, ok := message["content"].(string); ok {
					content = value
				}
			}
		}
	}

	var totalTokens *int
	if metrics.PromptTokens != nil && metrics.CompletionTokens != nil {
		total := *metrics.PromptTokens + *metrics.CompletionTokens
		totalTokens = &total
	}
	statusCode := metrics.Status
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	response := PlaygroundChatResponse{
		RequestID: requestIDFromContext(r.Context()),
		Model:     req.Model,
		Message: map[string]string{
			"role":          "assistant",
			"content":       content,
			"finish_reason": finishReason,
		},
		Metrics: PlaygroundMetrics{
			Status:           statusCode,
			TTFTMS:           metrics.TTFTMS,
			LatencyMS:        metrics.LatencyMS,
			PromptTokens:     metrics.PromptTokens,
			CompletionTokens: metrics.CompletionTokens,
			TotalTokens:      totalTokens,
		},
	}
	writeJSON(w, http.StatusOK, response)
}


func playgroundInternalStreamHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRequestAPIError(w, r, newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "NEXORA_METHOD_NOT_ALLOWED", "Method not allowed."))
		return
	}
	if !verifyInternalPlaygroundToken(r) {
		writeRequestAPIError(w, r, newAPIError(http.StatusUnauthorized, "authentication_error", "NEXORA_INTERNAL_AUTH_FAILED", "Internal playground authentication failed."))
		return
	}

	payload, apiErr := decodePlaygroundChatRequest(w, r)
	if apiErr != nil {
		writeRequestAPIError(w, r, apiErr)
		return
	}

	route, ok := modelCatalog.Get(strings.TrimSpace(payload.Model))
	if !ok {
		writeRequestAPIError(w, r, newAPIError(http.StatusServiceUnavailable, "service_unavailable", "NEXORA_MODEL_UNAVAILABLE", "Requested model is not available."))
		return
	}

	lease, admissionErr := admissionController.AdmitPlayground(r.Context(), payload.UserID, payload.ProjectID)
	if admissionErr != nil {
		writeRequestAPIError(w, r, admissionErr)
		return
	}
	defer lease.Release(context.WithoutCancel(r.Context()))

	req := &ChatCompletionRequest{
		Model:    strings.TrimSpace(payload.Model),
		Messages: payload.Messages,
		Stream:   true,
	}

	metrics, providerErr := inferenceProvider.Chat(r.Context(), req, route, w)
	if providerErr != nil {
		writeRequestAPIError(w, r, providerErr)
		return
	}
	if metrics == nil || !metrics.Completed {
		return
	}

	var totalTokens *int
	if metrics.PromptTokens != nil && metrics.CompletionTokens != nil {
		total := *metrics.PromptTokens + *metrics.CompletionTokens
		totalTokens = &total
	}
	statusCode := metrics.Status
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	summary := map[string]any{
		"request_id": requestIDFromContext(r.Context()),
		"model":      req.Model,
		"metrics": PlaygroundMetrics{
			Status:           statusCode,
			TTFTMS:           metrics.TTFTMS,
			LatencyMS:        metrics.LatencyMS,
			PromptTokens:     metrics.PromptTokens,
			CompletionTokens: metrics.CompletionTokens,
			TotalTokens:      totalTokens,
		},
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		return
	}

	_, _ = w.Write([]byte("\nevent: nexora_metrics\ndata: "))
	_, _ = w.Write(encoded)
	_, _ = w.Write([]byte("\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
