package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OpenRouterProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
	breaker *CircuitBreaker
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
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &OpenRouterProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  client,
		breaker: NewCircuitBreaker(
			getenvInt("UPSTREAM_CIRCUIT_FAILURE_THRESHOLD", 5),
			time.Duration(getenvInt("UPSTREAM_CIRCUIT_OPEN_SECONDS", 30))*time.Second,
		),
	}, nil
}

func (p *OpenRouterProvider) circuitBreaker() *CircuitBreaker {
	if p.breaker == nil {
		p.breaker = NewCircuitBreaker(5, 30*time.Second)
	}
	return p.breaker
}

func (p *OpenRouterProvider) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (p *OpenRouterProvider) SyncFreeModels(ctx context.Context, catalog *ModelCatalog) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := p.newRequest(ctx, http.MethodGet, "/models", nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("upstream model catalog request failed")
	}

	body, err := readLimitedBody(resp.Body, 4<<20)
	if err != nil {
		return err
	}
	var payload upstreamModelsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return err
	}

	models := make([]Model, 0, len(payload.Data)+1)
	models = append(models, Model{
		ID: "auto-free", Object: "model", OwnedBy: "nexora",
		DisplayName: "Auto Free", Status: "active", Free: true,
		Capabilities: map[string]bool{"text": true, "streaming": true},
	})
	for _, m := range payload.Data {
		if m.ID == "" ||
			!zeroPrice(m.Pricing.Prompt) ||
			!zeroPrice(m.Pricing.Completion) ||
			!containsString(m.Architecture.OutputModalities, "text") {
			continue
		}
		models = append(models, Model{
			ID:            m.ID,
			Object:        "model",
			OwnedBy:       "nexora",
			DisplayName:   m.Name,
			ContextLength: m.ContextLength,
			Status:        "active",
			Free:          true,
			Capabilities:  map[string]bool{"text": true, "streaming": true},
		})
	}
	catalog.Replace(models)
	return nil
}

func (p *OpenRouterProvider) Chat(ctx context.Context, req *ChatCompletionRequest, w http.ResponseWriter) *APIError {
	upstreamReq := *req
	publicModel := req.Model
	if upstreamReq.Model == "auto-free" {
		upstreamReq.Model = "openrouter/free"
	}
	payload, err := json.Marshal(upstreamReq)
	if err != nil {
		return newAPIError(
			http.StatusInternalServerError,
			"server_error",
			"NEXORA_INTERNAL_ERROR",
			"Unable to encode request.",
		)
	}

	if !req.Stream {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(
			ctx,
			time.Duration(getenvInt("UPSTREAM_NONSTREAM_TIMEOUT_SECONDS", 120))*time.Second,
		)
		defer cancel()
	}

	breaker := p.circuitBreaker()
	if !breaker.Allow(time.Now()) {
		return newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_UPSTREAM_CIRCUIT_OPEN",
			"Upstream provider is temporarily unavailable.",
		)
	}

	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		upReq, requestErr := p.newRequest(ctx, http.MethodPost, "/chat/completions", bytes.NewReader(payload))
		if requestErr != nil {
			breaker.Failure(time.Now())
			return newAPIError(
				http.StatusBadGateway,
				"upstream_error",
				"NEXORA_UPSTREAM_ERROR",
				"Unable to create upstream request.",
			)
		}
		if req.Stream {
			upReq.Header.Set("Accept", "text/event-stream")
		}

		resp, err = p.client.Do(upReq)
		if err != nil {
			breaker.Failure(time.Now())
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return newAPIError(
					http.StatusGatewayTimeout,
					"upstream_error",
					"NEXORA_UPSTREAM_TIMEOUT",
					"Upstream provider timed out.",
				)
			}
			if ctx.Err() != nil {
				return newAPIError(
					http.StatusBadGateway,
					"upstream_error",
					"NEXORA_UPSTREAM_CANCELLED",
					"Upstream request was cancelled.",
				)
			}
			if attempt == 0 && breaker.Allow(time.Now()) {
				continue
			}
			return newAPIError(
				http.StatusBadGateway,
				"upstream_error",
				"NEXORA_UPSTREAM_ERROR",
				"Upstream provider request failed.",
			)
		}

		if retryableUpstreamStatus(resp.StatusCode) && attempt == 0 {
			_ = resp.Body.Close()
			breaker.Failure(time.Now())
			if breaker.Allow(time.Now()) {
				continue
			}
		}
		break
	}

	if resp == nil {
		return newAPIError(
			http.StatusBadGateway,
			"upstream_error",
			"NEXORA_UPSTREAM_ERROR",
			"Upstream provider request failed.",
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusGatewayTimeout {
			breaker.Failure(time.Now())
		}
		return mapUpstreamStatus(resp.StatusCode)
	}
	breaker.Success()

	if req.Stream {
		mediaType, _, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if parseErr != nil || mediaType != "text/event-stream" {
			return newAPIError(
				http.StatusBadGateway,
				"upstream_error",
				"NEXORA_UPSTREAM_INVALID_RESPONSE",
				"Upstream provider returned an invalid streaming response.",
			)
		}
		return p.streamResponse(ctx, resp, w, publicModel)
	}
	return p.jsonResponse(resp, w, publicModel)
}

func (p *OpenRouterProvider) jsonResponse(resp *http.Response, w http.ResponseWriter, publicModel string) *APIError {
	maxBytes := getenvInt("MAX_UPSTREAM_RESPONSE_BYTES", 8<<20)
	body, err := readLimitedBody(resp.Body, maxBytes)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			return newAPIError(
				http.StatusBadGateway,
				"upstream_error",
				"NEXORA_UPSTREAM_RESPONSE_TOO_LARGE",
				"Upstream provider returned an oversized response.",
			)
		}
		return newAPIError(
			http.StatusBadGateway,
			"upstream_error",
			"NEXORA_UPSTREAM_ERROR",
			"Unable to read upstream response.",
		)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return newAPIError(
			http.StatusBadGateway,
			"upstream_error",
			"NEXORA_UPSTREAM_INVALID_RESPONSE",
			"Upstream provider returned an invalid response.",
		)
	}
	payload["model"] = publicModel
	delete(payload, "provider")
	captureUsage(payload, w)
	writeJSON(w, http.StatusOK, payload)
	return nil
}

func (p *OpenRouterProvider) streamResponse(
	ctx context.Context,
	resp *http.Response,
	w http.ResponseWriter,
	publicModel string,
) *APIError {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return newAPIError(
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

	maxEventBytes := getenvInt("MAX_SSE_EVENT_BYTES", 1<<20)
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 32*1024), maxEventBytes)

	var event bytes.Buffer
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line := scanner.Text()
		processed := normalizeSSELine(line, publicModel, w)
		if event.Len()+len(processed)+1 > maxEventBytes {
			setWriterErrorClass(w, "upstream_stream_oversized")
			return nil
		}
		event.WriteString(processed)
		event.WriteByte('\n')

		if line == "" {
			if _, err := w.Write(event.Bytes()); err != nil {
				return nil
			}
			flusher.Flush()
			event.Reset()
		}
	}
	if scanner.Err() != nil {
		setWriterErrorClass(w, "upstream_stream_error")
		return nil
	}
	if event.Len() > 0 {
		if _, err := w.Write(event.Bytes()); err != nil {
			return nil
		}
		flusher.Flush()
	}
	return nil
}

func normalizeSSELine(line, publicModel string, w http.ResponseWriter) string {
	if !strings.HasPrefix(line, "data:") {
		return line
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" || data == "[DONE]" {
		return line
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return line
	}
	payload["model"] = publicModel
	delete(payload, "provider")
	captureUsage(payload, w)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return line
	}
	return "data: " + string(encoded)
}

func captureUsage(payload map[string]any, w http.ResponseWriter) {
	usage, ok := payload["usage"].(map[string]any)
	if !ok {
		return
	}
	prompt, promptOK := numericInt(usage["prompt_tokens"])
	completion, completionOK := numericInt(usage["completion_tokens"])
	if !promptOK && !completionOK {
		return
	}
	var promptPtr, completionPtr *int
	if promptOK {
		promptPtr = &prompt
	}
	if completionOK {
		completionPtr = &completion
	}
	if writer, ok := w.(interface{ SetUsage(*int, *int) }); ok {
		writer.SetUsage(promptPtr, completionPtr)
	}
}

func numericInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case json.Number:
		number, err := typed.Int64()
		return int(number), err == nil
	default:
		return 0, false
	}
}

var errResponseTooLarge = errors.New("response body exceeds configured limit")

func readLimitedBody(reader io.Reader, limit int) ([]byte, error) {
	if limit < 1 {
		return nil, errResponseTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, errResponseTooLarge
	}
	return body, nil
}

func retryableUpstreamStatus(status int) bool {
	return status == http.StatusRequestTimeout ||
		status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout ||
		status >= 500
}

func setWriterErrorClass(w http.ResponseWriter, value string) {
	if writer, ok := w.(errorClassWriter); ok {
		writer.SetErrorClass(value)
	}
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
