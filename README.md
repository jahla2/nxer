# Nexr AI Service

Nexr AI Service is a production-ready, provider-agnostic AI infrastructure service that gives applications a single, stable, OpenAI-compatible API for AI inference.

Instead of coupling applications directly to individual AI providers, Nexr abstracts model access, routing, authentication, quotas, reliability, and usage management behind one consistent service.

This allows teams to change models, add new inference backends, and introduce failover strategies without changing client integrations.

## Architecture

- React + TypeScript developer console
- Go high-performance inference service
- Python FastAPI control plane
- Celery + Redis background processing
- PostgreSQL authoritative persistence
- Redis for rate limiting, quotas, caching, locks, and idempotency
- Private Nexr Model Router
- Docker Compose + Nginx + Cloudflare Tunnel

## Public API

Applications integrate only with Nexr:

```text
POST /v1/chat/completions
GET  /v1/models
```

Example:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $NEXR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"auto-free","messages":[{"role":"user","content":"Hello"}]}'
```

Public model IDs remain Nexr-owned:

```text
auto-free
nexr/general-1
nexr/coding-1
nexr/reasoning-1
```

Internal model providers, credentials, routing logic, upstream IDs, failover rules, and infrastructure remain private.

## Why Nexr

Nexr acts as the AI service boundary between client applications and underlying inference infrastructure.

Applications only need:

```text
API URL
API Key
Model ID
Request
Response
```

Everything behind that boundary can evolve independently.

This keeps integrations simple while allowing Nexr to become more scalable, reliable, secure, and intelligent without breaking existing applications.

## Run Locally

```bash
git checkout main
git pull
copy .env.example .env
docker compose -f infra/docker-compose.yml up --build
```

Open:

```text
http://localhost:8080
```

Create a project, generate a Nexr API key, and start sending requests.

## Production

```bash
cp .env.production.example .env.production
docker compose -f infra/docker-compose.prod.yml up -d --build
```

Never expose internal provider credentials, routing metadata, database secrets, Redis credentials, or signing secrets.
