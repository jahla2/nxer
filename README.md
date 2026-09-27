# Nexora AI Gateway

Nexora AI Gateway is a reusable, white-label, OpenAI-compatible API gateway for free-model inference across multiple client applications.

## Architecture
- React + TypeScript developer console
- Go data plane for concurrent inference proxying and SSE streaming
- Python FastAPI control plane
- Celery + Redis background work
- PostgreSQL authoritative persistence
- Redis for limits, quotas, idempotency, locks, and hot metadata
- OpenRouter adapter for V1 free-model inference
- Docker Compose + Nginx + Cloudflare Tunnel deployment

## Quick start
1. Copy `.env.example` to `.env` and fill server-side secrets.
2. Run `docker compose -f infra/docker-compose.yml up --build`.
3. Check `/health` on the gateway and control API.

## Security
Never commit `.env`, provider credentials, signing secrets, database passwords, or Redis credentials.

## Client examples

```bash
curl http://localhost/v1/chat/completions \
  -H "Authorization: Bearer $NEXORA_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000" \
  -d '{"model":"auto-free","messages":[{"role":"user","content":"Hello"}]}'
```

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost/v1", api_key="nxa_live_...")
print(client.chat.completions.create(model="auto-free", messages=[{"role":"user","content":"Hello"}]))
```

```js
import OpenAI from "openai";
const client = new OpenAI({ baseURL: "http://localhost/v1", apiKey: "nxa_live_..." });
const result = await client.chat.completions.create({ model: "auto-free", messages: [{ role: "user", content: "Hello" }] });
```

## Production validation
Run migrations in order before starting services. Validate `/ready`, verify the free-model catalog, run the security/load suites, and perform a database backup/restore drill before exposing the Cloudflare hostname.
