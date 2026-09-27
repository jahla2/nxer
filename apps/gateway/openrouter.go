package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type OpenRouterProvider struct {
	baseURL        string
	apiKey         string
	client         *http.Client
	retryPolicy    RetryPolicy
	breaker        *CircuitBreaker
	requestTimeout time.Duration
	streamTimeout  time.Duration
	catalogTimeout time.Duration
}

func (p *OpenRouterProvider) Key() string {
	return openRouterProviderKey
}

type ProviderMetrics struct {
	Status           int
	TTFTMS           *int
	LatencyMS        *int
	PromptTokens     *int
	CompletionTokens *int
	Completed        bool
}

type upstreamModelsResponse struct {
	Data []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		ContextLength int    `json:"context_length"`
		Pricing       struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
		Architecture struct {
			OutputModalities []string `json:"output_modalities"`
		} `json:"architecture"`
	} `json:"data"`
}

func NewOpenRouterProvider(baseURL, apiKey string) (*OpenRouterProvider, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("OPENROUTER_API_KEY is required")
	}

	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("UPSTREAM_BASE_URL must be a valid HTTPS URL")
	}

	dialTimeout := time.Duration(getenvInt("PROVIDER_DIAL_TIMEOUT_SECONDS", 5)) * time.Second
	tlsTimeout := time.Duration(getenvInt("PROVIDER_TLS_TIMEOUT_SECONDS", 5)) * time.Second
	responseHeaderTimeout := time.Duration(getenvInt("PROVIDER_RESPONSE_HEADER_TIMEOUT_SECONDS", 30)) * time.Second
	idleTimeout := time.Duration(getenvInt("PROVIDER_IDLE_CONN_TIMEOUT_SECONDS", 90)) * time.Second

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          getenvInt("PROVIDER_MAX_IDLE_CONNS", 100),
		MaxIdleConnsPerHost:   getenvInt("PROVIDER_MAX_IDLE_CONNS_PER_HOST", 20),
		IdleConnTimeout:       idleTimeout,
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &OpenRouterProvider{
		baseURL:        strings.TrimRight(baseURL, "/"),
		apiKey:         apiKey,
		retryPolicy:    NewRetryPolicyFromEnv(),
		breaker:        NewCircuitBreakerFromEnv(),
		requestTimeout: time.Duration(getenvInt("PROVIDER_REQUEST_TIMEOUT_SECONDS", 90)) * time.Second,
		streamTimeout:  time.Duration(getenvInt("PROVIDER_STREAM_MAX_DURATION_SECONDS", 300)) * time.Second,
		catalogTimeout: time.Duration(getenvInt("PROVIDER_CATALOG_TIMEOUT_SECONDS", 15)) * time.Second,
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (p *OpenRouterProvider) newRequest(
	ctx context.Context,
	method, path string,
	body io.Reader,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (p *OpenRouterProvider) ListFreeModels(ctx context.Context) ([]DiscoveredModel, error) {
	timeout := p.catalogTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	policy := p.effectiveRetryPolicy()
	var resp *http.Response
	var err error

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		req, requestErr := p.newRequest(ctx, http.MethodGet, "/models", nil)
		if requestErr != nil {
			return nil, requestErr
		}

		resp, err = p.client.Do(req)
		if err != nil {
			if ctx.Err() != nil || attempt >= policy.MaxAttempts {
				return nil, err
			}
			delay := policy.Backoff(attempt, "")
			logProviderRetry(ctx, p.Key(), attempt, policy.MaxAttempts, delay, "catalog_transport_error")
			if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			break
		}

		statusCode := resp.StatusCode
		retryAfter := resp.Header.Get("Retry-After")
		drainAndClose(resp.Body)
		resp = nil

		if !isRetryableProviderStatus(statusCode) || attempt >= policy.MaxAttempts {
			return nil, errors.New("upstream model catalog request failed")
		}

		delay := policy.Backoff(attempt, retryAfter)
		logProviderRetry(
			ctx,
			p.Key(),
			attempt,
			policy.MaxAttempts,
			delay,
			"catalog_status_"+strconv.Itoa(statusCode),
		)
		if sleepErr := sleepWithContext(ctx, delay); sleepErr != nil {
			return nil, sleepErr
		}
	}

	if resp == nil {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("upstream model catalog request failed")
	}
	defer resp.Body.Close()

	var payload upstreamModelsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}

	models := make([]DiscoveredModel, 0, len(payload.Data))
	for _, model := range payload.Data {
		if model.ID == "" ||
			!zeroPrice(model.Pricing.Prompt) ||
			!zeroPrice(model.Pricing.Completion) ||
			!containsString(model.Architecture.OutputModalities, "text") {
			continue
		}

		models = append(models, DiscoveredModel{
			UpstreamID:    model.ID,
			DisplayName:   model.Name,
			ContextLength: model.ContextLength,
			Capabilities: map[string]bool{
				"text":      true,
				"streaming": true,
			},
		})
	}

	return models, nil
}

func (p *OpenRouterProvider) Chat(
	ctx context.Context,
	req *ChatCompletionRequest,
	route Model,
	w http.ResponseWriter,
) (*ProviderMetrics, *APIError) {
	start := time.Now()

	if route.ProviderKey != openRouterProviderKey || strings.TrimSpace(route.UpstreamID) == "" {
		return metricsFromStart(start, http.StatusServiceUnavailable, false), newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_ROUTE_UNAVAILABLE",
			"Requested model route is unavailable.",
		)
	}

	permit, allowed := p.acquireCircuitPermit()
	if !allowed {
		gatewayLogger.Warn(
			"provider circuit open",
			"event", "provider_circuit_open",
			"request_id", requestIDFromContext(ctx),
			"provider", p.Key(),
		)
		return metricsFromStart(start, http.StatusServiceUnavailable, false), newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_PROVIDER_CIRCUIT_OPEN",
			"Model provider is temporarily unavailable.",
		)
	}

	requestCtx, cancel := p.withRequestTimeout(ctx, req.Stream)
	defer cancel()

	upstreamReq := *req
	upstreamReq.Model = route.UpstreamID

	payload, err := json.Marshal(upstreamReq)
	if err != nil {
		permit.Neutral()
		return metricsFromStart(start, http.StatusInternalServerError, false), newAPIError(
			http.StatusInternalServerError,
			"server_error",
			"NEXORA_INTERNAL_ERROR",
			"Unable to encode request.",
		)
	}

	policy := p.effectiveRetryPolicy()
	if permit != nil && permit.halfOpen {
		policy.MaxAttempts = 1
	}

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		resp, wroteRequest, requestErr := p.doChatAttempt(
			requestCtx,
			payload,
			req.Stream,
		)

		if requestErr != nil {
			if errors.Is(ctx.Err(), context.Canceled) || errors.Is(requestErr, context.Canceled) {
				permit.Neutral()
				return metricsFromStart(start, 499, false), nil
			}

			apiErr := newAPIError(
				http.StatusBadGateway,
				"upstream_error",
				"NEXORA_UPSTREAM_ERROR",
				"Upstream provider request failed.",
			)
			if errors.Is(requestCtx.Err(), context.DeadlineExceeded) ||
				errors.Is(requestErr, context.DeadlineExceeded) {
				apiErr = newAPIError(
					http.StatusGatewayTimeout,
					"upstream_error",
					"NEXORA_UPSTREAM_TIMEOUT",
					"Upstream provider timed out.",
				)
			}

			canRetry := !wroteRequest &&
				attempt < policy.MaxAttempts &&
				requestCtx.Err() == nil
			if canRetry {
				delay := policy.Backoff(attempt, "")
				logProviderRetry(ctx, p.Key(), attempt, policy.MaxAttempts, delay, "transport_error")
				if err := sleepWithContext(requestCtx, delay); err == nil {
					continue
				}
			}

			permit.Failure()
			return metricsFromStart(start, apiErr.Status, false), apiErr
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			statusCode := resp.StatusCode
			retryAfter := resp.Header.Get("Retry-After")
			drainAndClose(resp.Body)

			apiErr := mapUpstreamStatus(statusCode)
			canRetry := isRetryableProviderStatus(statusCode) &&
				attempt < policy.MaxAttempts &&
				requestCtx.Err() == nil
			if canRetry {
				delay := policy.Backoff(attempt, retryAfter)
				logProviderRetry(
					ctx,
					p.Key(),
					attempt,
					policy.MaxAttempts,
					delay,
					"status_"+strconv.Itoa(statusCode),
				)
				if err := sleepWithContext(requestCtx, delay); err == nil {
					continue
				}
			}

			if isRetryableProviderStatus(statusCode) {
				permit.Failure()
			} else {
				permit.Success()
			}
			return metricsFromStart(start, apiErr.Status, false), apiErr
		}

		if req.Stream {
			contentType := strings.ToLower(resp.Header.Get("Content-Type"))
			if !strings.Contains(contentType, "text/event-stream") {
				drainAndClose(resp.Body)
				permit.Failure()
				return metricsFromStart(start, http.StatusBadGateway, false), newAPIError(
					http.StatusBadGateway,
					"upstream_error",
					"NEXORA_UPSTREAM_INVALID_RESPONSE",
					"Upstream provider returned an invalid streaming response.",
				)
			}

			metrics, apiErr := p.streamResponse(
				requestCtx,
				resp,
				route.ID,
				w,
				start,
			)
			_ = resp.Body.Close()

			if apiErr != nil {
				permit.Failure()
				return metrics, apiErr
			}
			if metrics == nil {
				permit.Failure()
				return metricsFromStart(start, http.StatusBadGateway, false), newAPIError(
					http.StatusBadGateway,
					"upstream_error",
					"NEXORA_UPSTREAM_ERROR",
					"Upstream provider response did not complete.",
				)
			}
			if metrics.Status == 499 {
				permit.Neutral()
				return metrics, nil
			}
			if !metrics.Completed {
				permit.Failure()
				return metrics, nil
			}

			permit.Success()
			return metrics, nil
		}

		metrics, apiErr := p.jsonResponse(resp, route.ID, w, start)
		_ = resp.Body.Close()
		if apiErr != nil || metrics == nil || !metrics.Completed {
			permit.Failure()
			return metrics, apiErr
		}

		permit.Success()
		return metrics, nil
	}

	permit.Failure()
	return metricsFromStart(start, http.StatusServiceUnavailable, false), newAPIError(
		http.StatusServiceUnavailable,
		"service_unavailable",
		"NEXORA_UPSTREAM_ERROR",
		"Model provider is temporarily unavailable.",
	)
}

func (p *OpenRouterProvider) acquireCircuitPermit() (*CircuitPermit, bool) {
	if p.breaker == nil {
		return &CircuitPermit{}, true
	}
	return p.breaker.Acquire()
}

func (p *OpenRouterProvider) effectiveRetryPolicy() RetryPolicy {
	policy := p.retryPolicy
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}
	if policy.MaxAttempts > 4 {
		policy.MaxAttempts = 4
	}
	if policy.MaxDelay < policy.BaseDelay {
		policy.MaxDelay = policy.BaseDelay
	}
	return policy
}

func (p *OpenRouterProvider) withRequestTimeout(
	ctx context.Context,
	stream bool,
) (context.Context, context.CancelFunc) {
	timeout := p.requestTimeout
	if stream {
		timeout = p.streamTimeout
	}
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (p *OpenRouterProvider) doChatAttempt(
	ctx context.Context,
	payload []byte,
	stream bool,
) (*http.Response, bool, error) {
	var wroteRequest atomic.Bool
	trace := &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				wroteRequest.Store(true)
			}
		},
	}

	attemptCtx := httptrace.WithClientTrace(ctx, trace)
	upReq, err := p.newRequest(
		attemptCtx,
		http.MethodPost,
		"/chat/completions",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, false, err
	}
	if stream {
		upReq.Header.Set("Accept", "text/event-stream")
	}

	resp, err := p.client.Do(upReq)
	return resp, wroteRequest.Load(), err
}

func logProviderRetry(
	ctx context.Context,
	provider string,
	attempt int,
	maxAttempts int,
	delay time.Duration,
	reason string,
) {
	gatewayLogger.Warn(
		"provider request retry",
		"event", "provider_retry",
		"request_id", requestIDFromContext(ctx),
		"provider", provider,
		"attempt", attempt,
		"max_attempts", maxAttempts,
		"delay_ms", delay.Milliseconds(),
		"reason", reason,
	)
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 32<<10))
	_ = body.Close()
}

func (p *OpenRouterProvider) jsonResponse(
	resp *http.Response,
	publicModelID string,
	w http.ResponseWriter,
	start time.Time,
) (*ProviderMetrics, *APIError) {
	firstRead := time.Time{}
	reader := &firstReadRecorder{
		reader: resp.Body,
		onFirstRead: func() {
			firstRead = time.Now()
		},
	}

	body, err := io.ReadAll(io.LimitReader(reader, 8<<20))
	if err != nil {
		return metricsFromStart(start, http.StatusBadGateway, false), newAPIError(
			http.StatusBadGateway,
			"upstream_error",
			"NEXORA_UPSTREAM_ERROR",
			"Unable to read upstream response.",
		)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return metricsFromStart(start, http.StatusBadGateway, false), newAPIError(
			http.StatusBadGateway,
			"upstream_error",
			"NEXORA_UPSTREAM_INVALID_RESPONSE",
			"Upstream provider returned an invalid response.",
		)
	}

	metrics := metricsFromStart(start, http.StatusOK, true)
	if !firstRead.IsZero() {
		metrics.TTFTMS = durationMillisPtr(firstRead.Sub(start))
	}
	metrics.PromptTokens, metrics.CompletionTokens = extractUsageTokens(payload)

	sanitizeChatPayload(payload, publicModelID, newCompletionID())
	writeJSON(w, http.StatusOK, payload)
	return metrics, nil
}

func (p *OpenRouterProvider) streamResponse(
	ctx context.Context,
	resp *http.Response,
	publicModelID string,
	w http.ResponseWriter,
	start time.Time,
) (*ProviderMetrics, *APIError) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return metricsFromStart(start, http.StatusInternalServerError, false), newAPIError(
			http.StatusInternalServerError,
			"server_error",
			"NEXORA_STREAMING_UNSUPPORTED",
			"Streaming is unavailable.",
		)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	metrics := &ProviderMetrics{Status: http.StatusOK}
	reader := bufio.NewReaderSize(resp.Body, 32*1024)
	firstDataSeen := false
	sawDone := false
	publicCompletionID := newCompletionID()

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			select {
			case <-ctx.Done():
				metrics.Status = 499
				metrics.Completed = false
				metrics.LatencyMS = durationMillisPtr(time.Since(start))
				return metrics, nil
			default:
			}

			sanitized, dataLine, sanitizeErr := sanitizeSSELine(
				line,
				publicModelID,
				publicCompletionID,
				metrics,
			)
			if sanitizeErr != nil {
				metrics.Status = http.StatusBadGateway
				metrics.Completed = false
				metrics.LatencyMS = durationMillisPtr(time.Since(start))
				return metrics, nil
			}

			if dataLine && isDoneSSELine(sanitized) {
				sawDone = true
			}
			if dataLine && !firstDataSeen && !isDoneSSELine(sanitized) {
				firstDataSeen = true
				metrics.TTFTMS = durationMillisPtr(time.Since(start))
			}

			if len(sanitized) > 0 {
				if _, writeErr := w.Write(sanitized); writeErr != nil {
					metrics.Status = 499
					metrics.Completed = false
					metrics.LatencyMS = durationMillisPtr(time.Since(start))
					return metrics, nil
				}
				flusher.Flush()
			}
		}

		if err != nil {
			metrics.LatencyMS = durationMillisPtr(time.Since(start))
			if errors.Is(err, io.EOF) {
				metrics.Completed = sawDone
				if !sawDone {
					metrics.Status = http.StatusBadGateway
				}
				return metrics, nil
			}
			metrics.Status = http.StatusBadGateway
			metrics.Completed = false
			return metrics, nil
		}
	}
}

func sanitizeSSELine(
	line []byte,
	publicModelID string,
	publicCompletionID string,
	metrics *ProviderMetrics,
) ([]byte, bool, error) {
	trimmed := strings.TrimSpace(string(line))
	if trimmed == "" {
		return []byte("\n"), false, nil
	}

	if !strings.HasPrefix(trimmed, "data:") {
		// Drop upstream comments/event metadata so provider-specific details
		// cannot escape through the white-label streaming contract.
		return nil, false, nil
	}

	payloadText := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payloadText == "[DONE]" {
		return []byte("data: [DONE]\n"), true, nil
	}
	if payloadText == "" {
		return nil, false, nil
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadText), &payload); err != nil {
		return nil, true, err
	}

	prompt, completion := extractUsageTokens(payload)
	if prompt != nil {
		metrics.PromptTokens = prompt
	}
	if completion != nil {
		metrics.CompletionTokens = completion
	}

	sanitizeChatPayload(payload, publicModelID, publicCompletionID)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, true, err
	}
	return append(append([]byte("data: "), encoded...), '\n'), true, nil
}

func sanitizeChatPayload(payload map[string]any, publicModelID, publicCompletionID string) {
	if payload == nil {
		return
	}

	payload["id"] = publicCompletionID
	payload["model"] = publicModelID

	for _, key := range []string{
		"provider",
		"provider_name",
		"upstream_id",
		"generation_id",
	} {
		delete(payload, key)
	}

	if usage, ok := payload["usage"].(map[string]any); ok {
		for _, key := range []string{
			"cost",
			"cost_details",
			"upstream_inference_cost",
			"is_byok",
		} {
			delete(usage, key)
		}
	}
}

func newCompletionID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "chatcmpl_nxa_unavailable"
	}
	return "chatcmpl_nxa_" + hex.EncodeToString(raw[:])
}

type firstReadRecorder struct {
	reader      io.Reader
	onFirstRead func()
	seen        bool
}

func (r *firstReadRecorder) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 && !r.seen {
		r.seen = true
		if r.onFirstRead != nil {
			r.onFirstRead()
		}
	}
	return n, err
}

func metricsFromStart(start time.Time, status int, completed bool) *ProviderMetrics {
	return &ProviderMetrics{
		Status:    status,
		LatencyMS: durationMillisPtr(time.Since(start)),
		Completed: completed,
	}
}

func durationMillisPtr(duration time.Duration) *int {
	value := int(duration.Milliseconds())
	return &value
}

func extractUsageTokens(payload any) (*int, *int) {
	root, ok := payload.(map[string]any)
	if !ok {
		return nil, nil
	}
	usage, ok := root["usage"].(map[string]any)
	if !ok {
		return nil, nil
	}
	return jsonNumberInt(usage["prompt_tokens"]), jsonNumberInt(usage["completion_tokens"])
}

func jsonNumberInt(value any) *int {
	switch v := value.(type) {
	case float64:
		n := int(v)
		return &n
	case int:
		n := v
		return &n
	case json.Number:
		if parsed, err := strconv.Atoi(v.String()); err == nil {
			return &parsed
		}
	}
	return nil
}

func isDoneSSELine(line []byte) bool {
	return strings.TrimSpace(string(line)) == "data: [DONE]"
}

func mapUpstreamStatus(status int) *APIError {
	switch status {
	case http.StatusTooManyRequests:
		return newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_UPSTREAM_CAPACITY",
			"Upstream free-model capacity is temporarily unavailable.",
		)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return newAPIError(
			http.StatusGatewayTimeout,
			"upstream_error",
			"NEXORA_UPSTREAM_TIMEOUT",
			"Upstream provider timed out.",
		)
	default:
		if status >= 500 {
			return newAPIError(
				http.StatusBadGateway,
				"upstream_error",
				"NEXORA_UPSTREAM_ERROR",
				"Upstream provider is temporarily unavailable.",
			)
		}
		return newAPIError(
			http.StatusBadGateway,
			"upstream_error",
			"NEXORA_UPSTREAM_REJECTED",
			"Upstream provider rejected the request.",
		)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func zeroPrice(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return err == nil && parsed == 0
}
