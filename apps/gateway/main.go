package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", healthHandler)
	mux.HandleFunc("/v1/models", modelsHandler)
	mux.HandleFunc("/v1/chat/completions", chatHandler)

	server := &http.Server{
		Addr:              getenv("GATEWAY_ADDR", ":8080"),
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	log.Printf("gateway listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "service": "gateway", "time": time.Now().UTC().Format(time.RFC3339),
	})
}

func modelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "NEXORA_METHOD_NOT_ALLOWED", "Method not allowed.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []any{}})
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "NEXORA_METHOD_NOT_ALLOWED", "Method not allowed.")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "service_unavailable", "NEXORA_PROVIDER_NOT_CONFIGURED", "Inference provider is not configured yet.")
}

func writeError(w http.ResponseWriter, status int, typ, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"type": typ, "code": code, "message": message, "request_id": "req_nxa_unavailable",
	}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("method=%s path=%s duration_ms=%d", r.Method, r.URL.Path, time.Since(start).Milliseconds())
	})
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" { return value }
	return fallback
}
