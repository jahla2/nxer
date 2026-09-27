import os
from celery import Celery

broker = os.getenv("REDIS_URL", "redis://:nexora@redis:6379/0")
app = Celery("nexora", broker=broker, backend=broker)

@app.task(name="nexora.healthcheck")
def healthcheck() -> dict[str, str]:
    return {"status": "ok"}
