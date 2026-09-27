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
var openRouterProvider *OpenRouterProvider
var idempotencyGuard *IdempotencyGuard

func main() {
	var err error
	apiKeyAuthenticator, err = NewAPIKeyAuthenticator(getenv("DATABASE_URL", ""), getenv("REDIS_URL", ""), getenv("API_KEY_HASH_PEPPER", ""))
	if err != nil { log.Fatalf("gateway authentication configuration invalid: %v", err) }
	defer apiKeyAuthenticator.Close()
	admissionController, err = NewAdmissionController(getenv("REDIS_URL", ""))
	if err != nil { log.Fatalf("gateway admission configuration invalid: %v", err) }
	defer admissionController.Close()
	idempotencyGuard = NewIdempotencyGuard(admissionController.RedisClient())
	openRouterProvider, err = NewOpenRouterProvider(getenv("UPSTREAM_BASE_URL","https://openrouter.ai/api/v1"),getenv("OPENROUTER_API_KEY",""))
	if err != nil { log.Fatalf("gateway provider configuration invalid: %v",err) }
	if err := openRouterProvider.SyncFreeModels(context.Background(),modelCatalog); err != nil { log.Fatalf("initial free-model catalog sync failed: %v",err) }
	go syncModelCatalog(openRouterProvider,modelCatalog,time.Duration(getenvInt("FREE_MODEL_SYNC_INTERVAL_MINUTES",10))*time.Minute)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler)
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

func readyHandler(w http.ResponseWriter, r *http.Request) {
	ctx,cancel:=context.WithTimeout(r.Context(),2*time.Second); defer cancel()
	if apiKeyAuthenticator==nil || admissionController==nil || apiKeyAuthenticator.Ping(ctx)!=nil || admissionController.RedisClient().Ping(ctx).Err()!=nil {
		writeJSON(w,http.StatusServiceUnavailable,map[string]any{"status":"not_ready","service":"gateway"}); return
	}
	writeJSON(w,http.StatusOK,map[string]any{"status":"ok","service":"gateway"})
}

func modelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeAPIError(w,newAPIError(http.StatusMethodNotAllowed,"method_not_allowed","NEXORA_METHOD_NOT_ALLOWED","Method not allowed.")); return }
	principal, ok := r.Context().Value(apiKeyContextKey{}).(*APIKeyPrincipal)
	if !ok { writeAPIError(w,newAPIError(http.StatusUnauthorized,"authentication_error","NEXORA_INVALID_API_KEY","A valid Nexora API key is required.")); return }
	writeJSON(w,http.StatusOK,map[string]any{"object":"list","data":filterModelsForPrincipal(modelCatalog.ListActiveFree(),principal)})
}

func filterModelsForPrincipal(models []Model, principal *APIKeyPrincipal) []Model {
	if principal == nil {
		return []Model{}
	}
	if principal.AllowAllFreeModels {
		return models
	}
	filtered := make([]Model,0,len(models))
	for _, model := range models {
		if principal.AllowsModel(model.ID) {
			filtered = append(filtered,model)
		}
	}
	return filtered
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { writeAPIError(w,newAPIError(http.StatusMethodNotAllowed,"method_not_allowed","NEXORA_METHOD_NOT_ALLOWED","Method not allowed.")); return }
	req, apiErr := decodeChatCompletionRequest(w,r)
	if apiErr != nil { writeAPIError(w,apiErr); return }
	principal, ok := r.Context().Value(apiKeyContextKey{}).(*APIKeyPrincipal)
	if !ok { writeAPIError(w,newAPIError(http.StatusUnauthorized,"authentication_error","NEXORA_INVALID_API_KEY","A valid Nexora API key is required.")); return }
	if _, ok := modelCatalog.Get(req.Model); !ok { writeAPIError(w,newAPIError(http.StatusServiceUnavailable,"service_unavailable","NEXORA_MODEL_UNAVAILABLE","Requested model is not available.")); return }
	if !principal.AllowsModel(req.Model) { writeAPIError(w,newAPIError(http.StatusForbidden,"permission_error","NEXORA_MODEL_NOT_ALLOWED","This API key is not permitted to use the requested model.")); return }

	idemDecision, idemErr := idempotencyGuard.Begin(r,principal,req)
	if idemErr!=nil { writeAPIError(w,idemErr); return }
	if idemDecision.Replay!=nil { writeCachedIdempotentResponse(w,idemDecision.Replay); return }
	reservation:=idemDecision.Reservation

	lease, admissionErr := admissionController.Admit(r.Context(), principal)
	if admissionErr != nil {
		if reservation!=nil { reservation.Fail(context.WithoutCancel(r.Context())) }
		writeAPIError(w, admissionErr)
		return
	}
	defer lease.Release(context.WithoutCancel(r.Context()))

	if req.Stream {
		if providerErr:=openRouterProvider.Chat(r.Context(),req,w); providerErr!=nil {
			if reservation!=nil { reservation.Fail(context.WithoutCancel(r.Context())) }
			writeAPIError(w,providerErr)
			return
		}
		if reservation!=nil {
			if finalizeErr:=reservation.CompleteStream(context.WithoutCancel(r.Context())); finalizeErr!=nil {
				log.Printf("idempotency stream finalization failed code=%s",finalizeErr.Code)
			}
		}
		return
	}

	if reservation==nil {
		if providerErr:=openRouterProvider.Chat(r.Context(),req,w); providerErr!=nil { writeAPIError(w,providerErr); return }
		return
	}

	buffered:=NewBufferedResponseWriter()
	if providerErr:=openRouterProvider.Chat(r.Context(),req,buffered); providerErr!=nil {
		reservation.Fail(context.WithoutCancel(r.Context()))
		writeAPIError(w,providerErr)
		return
	}
	if finalizeErr:=reservation.Complete(
		context.WithoutCancel(r.Context()),
		buffered.Status(),
		buffered.Header().Get("Content-Type"),
		buffered.Body(),
	); finalizeErr!=nil {
		log.Printf("idempotency response finalization failed code=%s",finalizeErr.Code)
	}
	buffered.FlushTo(w)
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

func syncModelCatalog(provider *OpenRouterProvider,catalog *ModelCatalog,interval time.Duration){
	if interval < time.Minute { interval=time.Minute }
	ticker:=time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if err:=provider.SyncFreeModels(context.Background(),catalog);err!=nil { log.Printf("free-model catalog sync failed: %v",err) }
	}
}
