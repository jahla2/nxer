# MVP load scenarios

Exercise the gateway with representative scenarios before release:

1. Sustained non-streaming traffic below configured RPM.
2. Streaming traffic up to the global concurrency ceiling.
3. Burst above per-key RPM and verify bounded 429 responses.
4. Multiple keys in one project above project concurrency.
5. Redis interruption during admission; requests must fail closed.
6. Client disconnect during SSE; upstream context must be cancelled.
7. Model-catalog refresh while requests are active.
8. Repeated duplicate starts with the same Idempotency-Key.

Capture p50/p95 gateway-added latency, error rate, active concurrency, and Redis/PostgreSQL resource use.
