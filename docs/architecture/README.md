# Architecture

Nexora uses a split control-plane/data-plane architecture.

Hot path:

Client -> Cloudflare/Nginx -> Go Gateway -> private provider adapter -> upstream AI -> sanitized streamed response

Control-plane responsibilities are kept outside the token streaming path. The Go gateway must remain independently usable if the React dashboard is unavailable.

## Provider boundary

External clients only use Nexora public model IDs such as `auto-free` and `nexora/<alias>`. PostgreSQL stores the private provider key and upstream route separately. The gateway resolves the public model to its internal route only after authentication and model-scope checks.

Provider identity, upstream model IDs, upstream completion IDs, provider-specific response metadata, and upstream SSE comments are not part of the public API contract. JSON and streaming responses are normalized back to Nexora model/completion identifiers before they leave the gateway.


## Provider resilience

Inference requests use bounded retries only before a successful response is exposed to the client. Explicit transient provider responses such as 429/5xx may be retried within the configured attempt budget. Transport failures are retried only when the Go HTTP trace confirms that the request was not written, preventing accidental duplicate generations after an ambiguous network failure.

The gateway applies separate deadlines for standard completions and long-lived streams, caps retry backoff, honors a bounded Retry-After value, and uses a generation-safe circuit breaker. When the circuit is open, requests fail fast with a Nexora 503 instead of piling onto an unhealthy provider. After the cooldown, exactly one half-open probe is allowed; stale concurrent permits cannot incorrectly close a newer open circuit.

Client disconnects are treated as neutral provider outcomes. Streaming requests are marked complete only after a valid `[DONE]` marker; disconnects or truncated streams release local request state without counting as provider health success.
