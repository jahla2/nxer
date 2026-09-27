# Nexora MVP security verification

Before release:

- Confirm provider credentials never appear in browser bundles, API responses, application logs, or CI output.
- Confirm raw Nexora API keys are returned only at creation and only digest + public prefix are persisted.
- Send malformed, expired, revoked and random keys and verify inference is denied.
- Verify Redis outage causes inference to fail closed.
- Verify paid and non-text upstream models are absent from `/v1/models`.
- Verify explicit unavailable model IDs fail instead of silently changing model.
- Exercise payload-size and output-token limits.
- Reuse an Idempotency-Key with a different body and verify HTTP 409.
- Run concurrent requests above key/project/global limits and verify HTTP 429.
- Verify prompt/completion bodies and Authorization headers are absent from logs.
- Restore a PostgreSQL backup into a clean instance and verify migrations and readiness.
