package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type telemetryContextKey struct{}

type RequestTelemetry struct {
	RequestID        string
	KeyID            string
	ProjectID        string
	UserID           string
	Model            string
	ErrorClass       string
	PromptTokens     *int
	CompletionTokens *int
}

type observedResponseWriter struct {
	http.ResponseWriter
	status     int
	wrote      bool
	firstWrite time.Time
	telemetry  *RequestTelemetry
}

func (w *observedResponseWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.status = status
	w.wrote = true
	w.firstWrite = time.Now()
	w.ResponseWriter.WriteHeader(status)
}

func (w *observedResponseWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *observedResponseWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *observedResponseWriter) RequestID() string {
	if w.telemetry == nil {
		return ""
	}
	return w.telemetry.RequestID
}

func (w *observedResponseWriter) SetErrorClass(value string) {
	if w.telemetry != nil {
		w.telemetry.ErrorClass = value
	}
}

func (w *observedResponseWriter) SetUsage(promptTokens, completionTokens *int) {
	if w.telemetry == nil {
		return
	}
	w.telemetry.PromptTokens = promptTokens
	w.telemetry.CompletionTokens = completionTokens
}

func telemetryFromContext(ctx context.Context) *RequestTelemetry {
	value, _ := ctx.Value(telemetryContextKey{}).(*RequestTelemetry)
	return value
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		state := &RequestTelemetry{RequestID: newRequestID()}
		r = r.WithContext(context.WithValue(r.Context(), telemetryContextKey{}, state))

		w.Header().Set("X-Request-ID", state.RequestID)
		observed := &observedResponseWriter{ResponseWriter: w, status: http.StatusOK, telemetry: state}
		next.ServeHTTP(observed, r)

		latency := time.Since(start)
		ttftMS := int64(-1)
		if !observed.firstWrite.IsZero() {
			ttftMS = observed.firstWrite.Sub(start).Milliseconds()
		}
		if state.ErrorClass == "" && observed.status >= 400 {
			state.ErrorClass = errorClassForStatus(observed.status)
		}

		entry := map[string]any{
			"event":      "http_request",
			"request_id": state.RequestID,
			"method":     r.Method,
			"path":       r.URL.Path,
			"status":     observed.status,
			"latency_ms": latency.Milliseconds(),
			"ttft_ms":    ttftMS,
		}
		if state.KeyID != "" {
			entry["key_id"] = state.KeyID
		}
		if state.ProjectID != "" {
			entry["project_id"] = state.ProjectID
		}
		if state.UserID != "" {
			entry["user_id"] = state.UserID
		}
		if state.Model != "" {
			entry["model"] = state.Model
		}
		if state.ErrorClass != "" {
			entry["error_class"] = state.ErrorClass
		}

		if encoded, err := json.Marshal(entry); err == nil {
			log.Print(string(encoded))
		}

		if usageRecorder != nil && r.URL.Path == "/v1/chat/completions" && state.KeyID != "" && state.Model != "" {
			usageRecorder.Record(UsageEvent{
				RequestID:        state.RequestID,
				APIKeyID:         state.KeyID,
				PublicModelID:    state.Model,
				Status:           observed.status,
				TTFTMS:           nullableMilliseconds(ttftMS),
				LatencyMS:        int(latency.Milliseconds()),
				PromptTokens:     state.PromptTokens,
				CompletionTokens: state.CompletionTokens,
				ErrorClass:       state.ErrorClass,
			})
		}
	})
}

func nullableMilliseconds(value int64) *int {
	if value < 0 {
		return nil
	}
	result := int(value)
	return &result
}

func errorClassForStatus(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "rate_limit"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "authentication"
	case status >= 500:
		return "server_or_upstream"
	case status >= 400:
		return "client_error"
	default:
		return ""
	}
}
