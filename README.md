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
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $NEXORA_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000" \
  -d '{"model":"auto-free","messages":[{"role":"user","content":"Hello"}]}'
```

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8080/v1", api_key="nxa_live_...")
print(client.chat.completions.create(model="auto-free", messages=[{"role":"user","content":"Hello"}]))
```

```js
import OpenAI from "openai";
const client = new OpenAI({ baseURL: "http://localhost:8080/v1", apiKey: "nxa_live_..." });
const result = await client.chat.completions.create({ model: "auto-free", messages: [{ role: "user", content: "Hello" }] });
```

## Production validation
Run migrations in order before starting services. Validate `/ready`, verify the free-model catalog, run the security/load suites, and perform a database backup/restore drill before exposing the Cloudflare hostname.


## Run locally

Prerequisites: Docker Desktop with Docker Compose v2 and an OpenRouter API key.

```bash
git checkout main
git pull
copy .env.example .env
```

On macOS/Linux use `cp .env.example .env`. Edit `.env` and set `OPENROUTER_API_KEY`. For a private local machine the provided development-only placeholders can boot the stack; replace all three secret placeholders before sharing or deploying it.

Start the full stack from the repository root:

```bash
docker compose -f infra/docker-compose.yml up --build
```

The tracked migration runner applies each SQL migration exactly once and safely skips already-applied versions. The local bootstrap then runs idempotently before the application services start. Open `http://localhost:8080`. In the console enter the same `CONTROL_ADMIN_TOKEN` from `.env`. A Local Project is bootstrapped. Create an API key in the API Keys module; the UI saves the one-time raw key locally for Models and Playground.

Useful local endpoints:

- Dashboard: `http://localhost:8080/`
- Gateway health/readiness: `http://localhost:8080/health`, `http://localhost:8080/ready`
- Control health/readiness: `http://localhost:8080/api/health`, `http://localhost:8080/api/ready`
- Models: `http://localhost:8080/v1/models`
- Chat: `http://localhost:8080/v1/chat/completions`

Stop with `docker compose -f infra/docker-compose.yml down`. Add `-v` only when you intentionally want to delete the local PostgreSQL volume.


### Local service configuration

The same root `.env` is injected into the Go gateway, FastAPI control plane, Celery worker/beat, migration runner, and local bootstrap. PostgreSQL reads `POSTGRES_DB`, `POSTGRES_USER`, and `POSTGRES_PASSWORD`; Redis reads `REDIS_PASSWORD`; application services use `DATABASE_URL` and `REDIS_URL`. Keep these values consistent when changing local credentials.

Long-running services use Docker's `unless-stopped` restart policy. Database migrations are tracked in `schema_migrations`, so restarting the stack does not reapply completed schema files.
