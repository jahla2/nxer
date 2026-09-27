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
