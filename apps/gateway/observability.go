package main

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
)

type requestIDContextKey struct{}
type requestTraceContextKey struct{}

type RequestTrace struct {
	APIKeyID string
	ProjectID string
}

type ObservedResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

var gatewayLogger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
	Level: slog.LevelInfo,
}))

func requestIDFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(requestIDContextKey{}).(string); ok && value != "" {
		return value
	}
	return ""
}

func requestTraceFromContext(ctx context.Context) *RequestTrace {
	if value, ok := ctx.Value(requestTraceContextKey{}).(*RequestTrace); ok {
		return value
	}
	return nil
}

func (w *ObservedResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *ObservedResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += n
	return n, err
}

func (w *ObservedResponseWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *ObservedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *ObservedResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *ObservedResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func requestObservabilityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := newRequestID()
		trace := &RequestTrace{}

		ctx := context.WithValue(r.Context(), requestIDContextKey{}, requestID)
		ctx = context.WithValue(ctx, requestTraceContextKey{}, trace)
		r = r.WithContext(ctx)

		w.Header().Set("X-Request-ID", requestID)
		observed := &ObservedResponseWriter{ResponseWriter: w}
		next.ServeHTTP(observed, r)

		fields := []any{
			"event", "http_request",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", observed.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"response_bytes", observed.bytes,
		}
		if trace.APIKeyID != "" {
			fields = append(fields, "api_key_id", trace.APIKeyID)
		}
		if trace.ProjectID != "" {
			fields = append(fields, "project_id", trace.ProjectID)
		}
		gatewayLogger.Info("request completed", fields...)
	})
}
