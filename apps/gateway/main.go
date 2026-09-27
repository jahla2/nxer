package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"
)

var modelCatalog = NewModelCatalog()
var apiKeyAuthenticator *APIKeyAuthenticator
var admissionController *AdmissionController
var inferenceProvider InferenceProvider
var idempotencyGuard *IdempotencyGuard
var usageRecorder *UsageRecorder
var modelCatalogStore *ModelCatalogStore

func main() {
	var err error
	apiKeyAuthenticator, err = NewAPIKeyAuthenticator(getenv("DATABASE_URL", ""), getenv("REDIS_URL", ""), getenv("API_KEY_HASH_PEPPER", ""))
	if err != nil {
		gatewayLogger.Error("gateway authentication configuration invalid","event","gateway_config_invalid","component","authentication","error",err.Error())
		os.Exit(1)
	}
	defer apiKeyAuthenticator.Close()
	admissionController, err = NewAdmissionController(getenv("REDIS_URL", ""))
	if err != nil {
		gatewayLogger.Error("gateway admission configuration invalid","event","gateway_config_invalid","component","admission","error",err.Error())
		os.Exit(1)
	}
	defer admissionController.Close()
	idempotencyGuard = NewIdempotencyGuard(admissionController.RedisClient())
	usageRecorder, err = NewUsageRecorder(getenv("DATABASE_URL", ""))
	if err != nil {
		gatewayLogger.Error("usage recorder configuration invalid","event","gateway_config_invalid","component","usage","error",err.Error())
		os.Exit(1)
	}
	defer usageRecorder.Close()

	modelCatalogStore, err = NewModelCatalogStore(getenv("DATABASE_URL", ""))
	if err != nil {
		gatewayLogger.Error("model catalog configuration invalid","event","gateway_config_invalid","component","catalog","error",err.Error())
		os.Exit(1)
	}
	defer modelCatalogStore.Close()

	inferenceProvider, err = NewOpenRouterProvider(getenv("UPSTREAM_BASE_URL","https://openrouter.ai/api/v1"),getenv("OPENROUTER_API_KEY",""))
	if err != nil {
		gatewayLogger.Error("gateway provider configuration invalid","event","gateway_config_invalid","component","provider","error",err.Error())
		os.Exit(1)
	}
	if err := refreshModelCatalog(context.Background(),inferenceProvider,modelCatalogStore,modelCatalog); err != nil {
		gatewayLogger.Error("initial free-model catalog sync failed","event","model_sync_failed","error",err.Error())
		os.Exit(1)
	}
	go syncModelCatalog(
		inferenceProvider,
		modelCatalogStore,
		modelCatalog,
		time.Duration(getenvInt("FREE_MODEL_SYNC_INTERVAL_MINUTES",10))*time.Minute,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler)
	mux.Handle("/v1/models", authMiddleware(apiKeyAuthenticator, http.HandlerFunc(modelsHandler)))
	mux.Handle("/v1/chat/completions", authMiddleware(apiKeyAuthenticator, http.HandlerFunc(chatHandler)))

	server := &http.Server{
		Addr: getenv("GATEWAY_ADDR", ":8080"),
		Handler: requestObservabilityMiddleware(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 15 * time.Second,
		IdleTimeout: 90 * time.Second,
	}
	gatewayLogger.Info("gateway listening","event","gateway_started","addr",server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		gatewayLogger.Error("gateway server failed","event","gateway_failed","error",err.Error())
		os.Exit(1)
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status":"ok","service":"gateway","time":time.Now().UTC().Format(time.RFC3339)})
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
	ctx,cancel:=context.WithTimeout(r.Context(),2*time.Second); defer cancel()
	if apiKeyAuthenticator==nil || admissionController==nil || usageRecorder==nil || modelCatalogStore==nil ||
		apiKeyAuthenticator.Ping(ctx)!=nil ||
		admissionController.RedisClient().Ping(ctx).Err()!=nil ||
		usageRecorder.Ping(ctx)!=nil ||
		modelCatalogStore.Ping(ctx)!=nil {
		writeJSON(w,http.StatusServiceUnavailable,map[string]any{"status":"not_ready","service":"gateway"}); return
	}
	writeJSON(w,http.StatusOK,map[string]any{"status":"ok","service":"gateway"})
}

func modelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { writeRequestAPIError(w,r,newAPIError(http.StatusMethodNotAllowed,"method_not_allowed","NEXORA_METHOD_NOT_ALLOWED","Method not allowed.")); return }
	principal, ok := r.Context().Value(apiKeyContextKey{}).(*APIKeyPrincipal)
	if !ok { writeRequestAPIError(w,r,newAPIError(http.StatusUnauthorized,"authentication_error","NEXORA_INVALID_API_KEY","A valid Nexora API key is required.")); return }
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
	if r.Method != http.MethodPost {
		writeRequestAPIError(w,r,newAPIError(http.StatusMethodNotAllowed,"method_not_allowed","NEXORA_METHOD_NOT_ALLOWED","Method not allowed."))
		return
	}
	req, apiErr := decodeChatCompletionRequest(w,r)
	if apiErr != nil {
		writeRequestAPIError(w,r,apiErr)
		return
	}
	principal, ok := r.Context().Value(apiKeyContextKey{}).(*APIKeyPrincipal)
	if !ok {
		writeRequestAPIError(w,r,newAPIError(http.StatusUnauthorized,"authentication_error","NEXORA_INVALID_API_KEY","A valid Nexora API key is required."))
		return
	}
	route, ok := modelCatalog.Get(req.Model)
	if !ok {
		writeRequestAPIError(w,r,newAPIError(http.StatusServiceUnavailable,"service_unavailable","NEXORA_MODEL_UNAVAILABLE","Requested model is not available."))
		return
	}
	if !principal.AllowsModel(req.Model) {
		writeRequestAPIError(w,r,newAPIError(http.StatusForbidden,"permission_error","NEXORA_MODEL_NOT_ALLOWED","This API key is not permitted to use the requested model."))
		return
	}

	idemDecision, idemErr := idempotencyGuard.Begin(r,principal,req)
	if idemErr!=nil {
		writeRequestAPIError(w,r,idemErr)
		return
	}
	if idemDecision.Replay!=nil {
		writeCachedIdempotentResponse(w,idemDecision.Replay)
		return
	}
	reservation:=idemDecision.Reservation

	lease, admissionErr := admissionController.Admit(r.Context(), principal)
	if admissionErr != nil {
		if reservation!=nil {
			reservation.Fail(context.WithoutCancel(r.Context()))
		}
		writeRequestAPIError(w,r,admissionErr)
		return
	}
	defer lease.Release(context.WithoutCancel(r.Context()))

	if req.Stream {
		metrics,providerErr:=inferenceProvider.Chat(r.Context(),req,route,w)
		enqueueUsageEvent(r,principal,req,metrics,providerErr)
		if providerErr!=nil {
			if reservation!=nil {
				reservation.Fail(context.WithoutCancel(r.Context()))
			}
			writeRequestAPIError(w,r,providerErr)
			return
		}
		if metrics==nil || !metrics.Completed {
			if reservation!=nil {
				reservation.Fail(context.WithoutCancel(r.Context()))
			}
			return
		}
		if reservation!=nil {
			if finalizeErr:=reservation.CompleteStream(context.WithoutCancel(r.Context())); finalizeErr!=nil {
				gatewayLogger.Warn(
					"idempotency stream finalization failed",
					"event","idempotency_finalize_failed",
					"request_id",requestIDFromContext(r.Context()),
					"code",finalizeErr.Code,
				)
			}
		}
		return
	}

	if reservation==nil {
		metrics,providerErr:=inferenceProvider.Chat(r.Context(),req,route,w)
		enqueueUsageEvent(r,principal,req,metrics,providerErr)
		if providerErr!=nil {
			writeRequestAPIError(w,r,providerErr)
		}
		return
	}

	buffered:=NewBufferedResponseWriter()
	metrics,providerErr:=inferenceProvider.Chat(r.Context(),req,route,buffered)
	enqueueUsageEvent(r,principal,req,metrics,providerErr)
	if providerErr!=nil {
		reservation.Fail(context.WithoutCancel(r.Context()))
		writeRequestAPIError(w,r,providerErr)
		return
	}
	if metrics==nil || !metrics.Completed {
		reservation.Fail(context.WithoutCancel(r.Context()))
		writeRequestAPIError(w,r,newAPIError(http.StatusBadGateway,"upstream_error","NEXORA_UPSTREAM_ERROR","Upstream provider response did not complete."))
		return
	}
	if finalizeErr:=reservation.Complete(
		context.WithoutCancel(r.Context()),
		buffered.Status(),
		buffered.Header().Get("Content-Type"),
		buffered.Body(),
	); finalizeErr!=nil {
		gatewayLogger.Warn(
			"idempotency response finalization failed",
			"event","idempotency_finalize_failed",
			"request_id",requestIDFromContext(r.Context()),
			"code",finalizeErr.Code,
		)
	}
	buffered.FlushTo(w)
}

func enqueueUsageEvent(
	r *http.Request,
	principal *APIKeyPrincipal,
	req *ChatCompletionRequest,
	metrics *ProviderMetrics,
	providerErr *APIError,
) {
	if usageRecorder==nil || r==nil || principal==nil || req==nil {
		return
	}
	statusCode:=http.StatusOK
	var ttft,latency,promptTokens,completionTokens *int
	if metrics!=nil {
		statusCode=metrics.Status
		ttft=metrics.TTFTMS
		latency=metrics.LatencyMS
		promptTokens=metrics.PromptTokens
		completionTokens=metrics.CompletionTokens
	}
	if providerErr!=nil {
		statusCode=providerErr.Status
	}
	requestID:=requestIDFromContext(r.Context())
	if requestID=="" {
		requestID=newRequestID()
	}
	if !usageRecorder.Enqueue(UsageEvent{
		RequestID:requestID,
		APIKeyID:principal.ID,
		ModelPublicID:req.Model,
		Status:statusCode,
		TTFTMS:ttft,
		LatencyMS:latency,
		PromptTokens:promptTokens,
		CompletionTokens:completionTokens,
	}) {
		gatewayLogger.Warn(
			"usage queue full",
			"event","usage_enqueue_dropped",
			"request_id",requestID,
			"api_key_id",principal.ID,
			"model",req.Model,
			"status",statusCode,
		)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type","application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func getenv(key,fallback string) string { if value:=os.Getenv(key); value!="" { return value }; return fallback }

func refreshModelCatalog(
	ctx context.Context,
	provider InferenceProvider,
	store *ModelCatalogStore,
	catalog *ModelCatalog,
) error {
	discovered, err := provider.ListFreeModels(ctx)
	if err != nil {
		return err
	}
	if err := store.Reconcile(ctx, provider.Key(), discovered); err != nil {
		return err
	}
	models, err := store.ListActiveFree(ctx)
	if err != nil {
		return err
	}
	catalog.Replace(models)
	return nil
}

func syncModelCatalog(
	provider InferenceProvider,
	store *ModelCatalogStore,
	catalog *ModelCatalog,
	interval time.Duration,
) {
	if interval < time.Minute {
		interval = time.Minute
	}
	ticker:=time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
		err:=refreshModelCatalog(ctx,provider,store,catalog)
		cancel()
		if err!=nil {
			gatewayLogger.Warn("free-model catalog sync failed","event","model_sync_failed","error",err.Error())
		}
	}
}
