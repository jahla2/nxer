package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

var modelCatalog = NewModelCatalog()
var apiKeyAuthenticator *APIKeyAuthenticator
var admissionController *AdmissionController
var openRouterProvider *OpenRouterProvider
var idempotencyGuard *IdempotencyGuard
var usageRecorder *UsageRecorder

func main() {
	var err error
	databaseURL := getenv("DATABASE_URL", "")

	apiKeyAuthenticator, err = NewAPIKeyAuthenticator(
		databaseURL,
		getenv("API_KEY_HASH_PEPPER", ""),
	)
	if err != nil {
		log.Fatalf("gateway authentication configuration invalid: %v", err)
	}
	defer apiKeyAuthenticator.Close()

	usageRecorder, err = NewUsageRecorder(
		databaseURL,
		getenvInt("USAGE_EVENT_QUEUE_SIZE", 512),
	)
	if err != nil {
		log.Fatalf("gateway usage recorder configuration invalid: %v", err)
	}
	defer usageRecorder.Close()

	admissionController, err = NewAdmissionController(getenv("REDIS_URL", ""))
	if err != nil {
		log.Fatalf("gateway admission configuration invalid: %v", err)
	}
	defer admissionController.Close()

	idempotencyGuard = NewIdempotencyGuard(admissionController.RedisClient())

	openRouterProvider, err = NewOpenRouterProvider(
		getenv("UPSTREAM_BASE_URL", "https://openrouter.ai/api/v1"),
		getenv("OPENROUTER_API_KEY", ""),
	)
	if err != nil {
		log.Fatalf("gateway provider configuration invalid: %v", err)
	}
	if err := openRouterProvider.SyncFreeModels(context.Background(), modelCatalog); err != nil {
		log.Printf("{\"event\":\"initial_model_sync_failed\"}")
	}
	go syncModelCatalog(
		openRouterProvider,
		modelCatalog,
		time.Duration(getenvInt("FREE_MODEL_SYNC_INTERVAL_MINUTES", 10))*time.Minute,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler)
	mux.Handle("/v1/models", authMiddleware(apiKeyAuthenticator, http.HandlerFunc(modelsHandler)))
	mux.Handle("/v1/chat/completions", authMiddleware(apiKeyAuthenticator, http.HandlerFunc(chatHandler)))

	server := &http.Server{
		Addr:              getenv("GATEWAY_ADDR", ":8080"),
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	log.Printf("{\"event\":\"gateway_listening\",\"addr\":%q}", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "gateway",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if apiKeyAuthenticator == nil ||
		admissionController == nil ||
		usageRecorder == nil ||
		apiKeyAuthenticator.Ping(ctx) != nil ||
		admissionController.RedisClient().Ping(ctx).Err() != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready", "service": "gateway",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "gateway"})
}

func modelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, newAPIError(
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"NEXORA_METHOD_NOT_ALLOWED",
			"Method not allowed.",
		))
		return
	}

	principal, ok := r.Context().Value(apiKeyContextKey{}).(*APIKeyPrincipal)
	if !ok {
		writeAPIError(w, newAPIError(
			http.StatusUnauthorized,
			"authentication_error",
			"NEXORA_INVALID_API_KEY",
			"A valid Nexora API key is required.",
		))
		return
	}

	allowed, err := apiKeyAuthenticator.AuthorizedModelIDs(r.Context(), principal)
	if err != nil {
		writeAPIError(w, newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_AUTH_UNAVAILABLE",
			"Model authorization is temporarily unavailable.",
		))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   modelCatalog.ListAuthorized(allowed),
	})
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, newAPIError(
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"NEXORA_METHOD_NOT_ALLOWED",
			"Method not allowed.",
		))
		return
	}

	req, apiErr := decodeChatCompletionRequest(w, r)
	if apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	if telemetry := telemetryFromContext(r.Context()); telemetry != nil {
		telemetry.Model = req.Model
	}

	if _, ok := modelCatalog.Get(req.Model); !ok {
		writeAPIError(w, newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_MODEL_UNAVAILABLE",
			"Requested model is not available.",
		))
		return
	}

	principal, ok := r.Context().Value(apiKeyContextKey{}).(*APIKeyPrincipal)
	if !ok {
		writeAPIError(w, newAPIError(
			http.StatusUnauthorized,
			"authentication_error",
			"NEXORA_INVALID_API_KEY",
			"A valid Nexora API key is required.",
		))
		return
	}

	if err := apiKeyAuthenticator.AuthorizeModel(r.Context(), principal, req.Model); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, newAPIError(
				http.StatusForbidden,
				"permission_error",
				"NEXORA_MODEL_NOT_ALLOWED",
				"Requested model is not allowed for this API key.",
			))
			return
		}
		writeAPIError(w, newAPIError(
			http.StatusServiceUnavailable,
			"service_unavailable",
			"NEXORA_AUTH_UNAVAILABLE",
			"Model authorization is temporarily unavailable.",
		))
		return
	}

	idemHandle, cached, idemErr := idempotencyGuard.Begin(
		r.Context(),
		principal,
		req,
		r.Header.Get("Idempotency-Key"),
	)
	if idemErr != nil {
		writeAPIError(w, idemErr)
		return
	}
	if cached != nil {
		writeCachedResponse(w, cached)
		return
	}

	lease, admissionErr := admissionController.Admit(r.Context(), principal, clientIP(r))
	if admissionErr != nil {
		if idemHandle != nil {
			idemHandle.Abort()
		}
		writeAPIError(w, admissionErr)
		return
	}
	defer lease.Release()

	if req.Stream {
		if providerErr := openRouterProvider.Chat(r.Context(), req, w); providerErr != nil {
			if idemHandle != nil {
				idemHandle.Abort()
			}
			writeAPIError(w, providerErr)
			return
		}
		if idemHandle != nil {
			idemHandle.FinishStream()
		}
		return
	}

	if idemHandle == nil {
		if providerErr := openRouterProvider.Chat(r.Context(), req, w); providerErr != nil {
			writeAPIError(w, providerErr)
		}
		return
	}

	buffered := newBufferedResponseWriter(w)
	if providerErr := openRouterProvider.Chat(r.Context(), req, buffered); providerErr != nil {
		idemHandle.Abort()
		writeAPIError(w, providerErr)
		return
	}
	cachedResponse := buffered.CachedResponse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	err := idemHandle.Complete(ctx, cachedResponse)
	cancel()
	if err != nil {
		w.Header().Set("Idempotency-Cache", "degraded")
		log.Printf("{\"event\":\"idempotency_complete_failed\",\"request_id\":%q}", requestIDFromWriter(w))
	}
	buffered.FlushTo(w)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func requestIDFromWriter(w http.ResponseWriter) string {
	if writer, ok := w.(requestIDWriter); ok {
		return writer.RequestID()
	}
	return ""
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		if comma := strings.IndexByte(forwarded, ','); comma >= 0 {
			forwarded = forwarded[:comma]
		}
		if value := strings.TrimSpace(forwarded); value != "" {
			return value
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func syncModelCatalog(provider *OpenRouterProvider, catalog *ModelCatalog, interval time.Duration) {
	if interval < time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if err := provider.SyncFreeModels(context.Background(), catalog); err != nil {
			log.Printf("{\"event\":\"free_model_catalog_sync_failed\"}")
		}
	}
}
