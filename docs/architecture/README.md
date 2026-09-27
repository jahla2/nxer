# Architecture

Nexora uses a split control-plane/data-plane architecture.

Hot path:

Client -> Cloudflare/Nginx -> Go Gateway -> private provider adapter -> upstream AI -> sanitized streamed response

Control-plane responsibilities are kept outside the token streaming path. The Go gateway must remain independently usable if the React dashboard is unavailable.

## Provider boundary

External clients only use Nexora public model IDs such as `auto-free` and `nexora/<alias>`. PostgreSQL stores the private provider key and upstream route separately. The gateway resolves the public model to its internal route only after authentication and model-scope checks.

Provider identity, upstream model IDs, upstream completion IDs, provider-specific response metadata, and upstream SSE comments are not part of the public API contract. JSON and streaming responses are normalized back to Nexora model/completion identifiers before they leave the gateway.
