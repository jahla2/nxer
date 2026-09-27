package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

var modelCatalog = NewModelCatalog()
var apiKeyAuthenticator *APIKeyAuthenticator
var admissionController *AdmissionController

func main() {
	var err error
	apiKeyAuthenticator, err = NewAPIKeyAuthenticator(getenv("DATABASE_URL", ""), getenv("API_KEY_HASH_PEPPER", ""))
	if err != nil { log.Fatalf("gateway authentication configuration invalid: %v", err) }
	defer apiKeyAuthenticator.Close()
	admissionController, err = NewAdmissionController(getenv("REDIS_URL", ""))
	if err != nil { log.Fatalf("gateway admission configuration invalid: %v", err) }
	defer admissionController.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", healthHandler)
	mux.Handle("/v1/models", authMiddleware(apiKeyAuthenticator, http.HandlerFunc(modelsHandler)))
	mux.Handle("/v1/chat/completions", authMiddleware(apiKeyAuthenticator, http.HandlerFunc(chatHandler)))

	server := &http.Server{
		Addr: getenv("GATEWAY_ADDR", ":8080"),
		Handler: loggingMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 90 * time.Second,
	}
	log.Printf("gateway listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed { log.Fatal(err) }
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status":"ok","service":"gateway","time":time.Now().UTC().Format(time.RFC3339)})
}

func modelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeAPIError(w,newAPIError(http.StatusMethodNotAllowed,"method_not_allowed","NEXORA_METHOD_NOT_ALLOWED","Method not allowed.")); return }
	writeJSON(w,http.StatusOK,map[string]any{"object":"list","data":modelCatalog.ListActiveFree()})
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { writeAPIError(w,newAPIError(http.StatusMethodNotAllowed,"method_not_allowed","NEXORA_METHOD_NOT_ALLOWED","Method not allowed.")); return }
	req, apiErr := decodeChatCompletionRequest(w,r)
	if apiErr != nil { writeAPIError(w,apiErr); return }
	if _, ok := modelCatalog.Get(req.Model); !ok { writeAPIError(w,newAPIError(http.StatusServiceUnavailable,"service_unavailable","NEXORA_MODEL_UNAVAILABLE","Requested model is not available.")); return }
	principal, ok := r.Context().Value(apiKeyContextKey{}).(*APIKeyPrincipal)
	if !ok { writeAPIError(w,newAPIError(http.StatusUnauthorized,"authentication_error","NEXORA_INVALID_API_KEY","A valid Nexora API key is required.")); return }
	lease, admissionErr := admissionController.Admit(r.Context(), principal)
	if admissionErr != nil { writeAPIError(w, admissionErr); return }
	defer lease.Release(context.WithoutCancel(r.Context()))
	writeAPIError(w,newAPIError(http.StatusServiceUnavailable,"service_unavailable","NEXORA_PROVIDER_NOT_CONFIGURED","Inference provider is not configured yet."))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type","application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		start:=time.Now()
		next.ServeHTTP(w,r)
		log.Printf("method=%s path=%s duration_ms=%d",r.Method,r.URL.Path,time.Since(start).Milliseconds())
	})
}

func getenv(key,fallback string) string { if value:=os.Getenv(key); value!="" { return value }; return fallback }