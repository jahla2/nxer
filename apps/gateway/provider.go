package main

import (
	"context"
	"net/http"
)

// InferenceProvider is the private adapter boundary between Nexora's public
// OpenAI-compatible contract and a concrete upstream inference service.
type InferenceProvider interface {
	Key() string
	ListFreeModels(ctx context.Context) ([]DiscoveredModel, error)
	Chat(
		ctx context.Context,
		req *ChatCompletionRequest,
		route Model,
		w http.ResponseWriter,
	) (*ProviderMetrics, *APIError)
}
