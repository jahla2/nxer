import os
from celery import Celery
import psycopg

broker = os.getenv("REDIS_URL", "redis://:nexora@redis:6379/0")
database_url = os.getenv("DATABASE_URL", "postgresql://nexora:nexora@postgres:5432/nexora")
app = Celery("nexora", broker=broker, backend=broker)
app.conf.beat_schedule = {
    "aggregate-usage-every-minute": {"task": "nexora.aggregate_usage", "schedule": 60.0},
    "cleanup-idempotency-hourly": {"task": "nexora.cleanup_idempotency", "schedule": 3600.0},
}

@app.task(name="nexora.healthcheck")
def healthcheck() -> dict[str, str]:
    return {"status": "ok"}

@app.task(name="nexora.aggregate_usage")
def aggregate_usage() -> dict[str, int]:
    with psycopg.connect(database_url) as conn, conn.cursor() as cur:
        cur.execute("""
            INSERT INTO usage_daily (usage_date, api_key_id, model_id, requests, prompt_tokens, completion_tokens)
            SELECT created_at::date, api_key_id, model_id, count(*),
                   coalesce(sum(prompt_tokens),0), coalesce(sum(completion_tokens),0)
            FROM usage_events
            WHERE created_at >= current_date - interval '1 day'
            GROUP BY created_at::date, api_key_id, model_id
            ON CONFLICT (usage_date, api_key_id, model_id) DO UPDATE SET
              requests=EXCLUDED.requests, prompt_tokens=EXCLUDED.prompt_tokens,
              completion_tokens=EXCLUDED.completion_tokens, updated_at=now()
        """)
        return {"rows": cur.rowcount}

@app.task(name="nexora.cleanup_idempotency")
def cleanup_idempotency() -> dict[str, int]:
    with psycopg.connect(database_url) as conn, conn.cursor() as cur:
        cur.execute("DELETE FROM idempotency_records WHERE expires_at < now()")
        return {"deleted": cur.rowcount}
