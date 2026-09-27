# Nexora V0.1.0-alpha Implementation Plan

This plan closes the gaps identified in the PRD and current repository in small, independently reviewed pull requests. Each phase must pass its own CI and verification gates before merge.

## Phase 1 — Foundation and local runtime hardening
- Upgrade the Go toolchain baseline to 1.26.x.
- Make SQL migrations tracked, ordered, rerunnable, and safe on an existing alpha database.
- Fix the `usage_daily` nullable-model aggregation key design.
- Make Docker Compose startup order deterministic: PostgreSQL/Redis → migrations → local bootstrap → application services.
- Parameterize PostgreSQL and Redis local credentials through `.env`.
- Add CI checks for gateway, control API, worker import/schedule, migrations, and Compose configuration.

Acceptance: an empty PostgreSQL database migrates successfully, a second migration run is a no-op, CI is green, and Docker Compose configuration validates.

## Phase 2 — Authentication, sessions, and tenant ownership
- Registration, login, current-user, refresh, logout, and password-reset hooks.
- Modern password hashing and hashed rotating refresh tokens.
- Secure HttpOnly cookie session flow.
- Authenticated project ownership and user-scoped API key/usage/request access.
- Optional development bootstrap account with a real password hash.

Acceptance: register/login/logout/refresh work, cross-user project/key access is denied, and normal React usage no longer requires `CONTROL_ADMIN_TOKEN`.

## Phase 3 — Project and API-key lifecycle
- Project create/rename/archive.
- API key create/show-once, rename, rotate, revoke, expiration, model scope/default model, limits, and last-used.
- Audit events and immediate Redis invalidation on sensitive key changes.

Acceptance: old keys fail after rotate/revoke, raw secrets are never persisted, and all operations are owner-scoped.

## Phase 4 — Go data-plane correctness and observability
- Complete idempotency response replay for non-streaming requests and duplicate-start prevention for streams.
- Clear failed pending idempotency reservations safely.
- Harden upstream redirects, size limits, SSE framing/content type, timeout/cancellation, bounded retry, and circuit breaking.
- Enforce project status/model scope and complete user/project/key/global admission policies.
- Structured JSON request logging and bounded asynchronous usage-event persistence.

Acceptance: provider/security tests, idempotency replay tests, revoke propagation tests, and gateway logging/usage tests pass.

## Phase 5 — Catalog, Celery, usage, and operations
- Persist free-model catalog in PostgreSQL/Redis.
- Celery periodic model synchronization, usage aggregation, retention, and operational housekeeping.
- Status/catalog freshness and audit/admin data endpoints.

Acceptance: model sync updates the dashboard without a gateway restart and background failures are observable.

## Phase 6 — React application and premium responsive UX
- Auth routes and session provider.
- Premium centered login/register screens.
- Responsive sidebar/topbar developer-console shell, project switcher, user menu, and working logout.
- Functional Overview, Projects, API Keys, Models, Playground, Usage, Requests, Settings, Status, Docs, and Admin surfaces.
- Loading, empty, success, error, modal, toast, accessibility, keyboard, and responsive states.

Acceptance: desktop/tablet/mobile golden path works without manual tokens or raw JSON-only screens.

## Phase 7 — Release validation
- Contract tests for OpenAI-compatible clients.
- Integration tests against PostgreSQL/Redis and mocked provider/SSE.
- 50–100 concurrent mocked streaming clients, burst-rate/idempotency tests, and disconnect recovery.
- Security, restart/recovery, backup/restore, and Cloudflare/Nginx end-to-end validation.

Acceptance: all V0.1.0-alpha PRD exit gates are evidenced before external users are invited.
