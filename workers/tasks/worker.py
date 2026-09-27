import os

from celery import Celery
from celery.signals import worker_ready

from catalog_sync import sync_model_catalog
from operations import JobRun
from settings import WorkerSettings
from usage_jobs import aggregate_usage, cleanup_housekeeping


settings = WorkerSettings.from_env()
broker = os.getenv("CELERY_BROKER_URL") or settings.redis_url
backend = os.getenv("CELERY_RESULT_BACKEND") or settings.redis_url

app = Celery("nexora", broker=broker, backend=backend)
app.conf.update(
    timezone="UTC",
    enable_utc=True,
    broker_connection_retry_on_startup=True,
    task_acks_late=True,
    task_reject_on_worker_lost=True,
    worker_prefetch_multiplier=1,
    task_track_started=True,
    task_ignore_result=True,
    task_soft_time_limit=110,
    task_time_limit=120,
)

app.conf.beat_schedule = {
    "sync-free-model-catalog": {
        "task": "nexora.sync_model_catalog",
        "schedule": float(settings.free_model_sync_interval_minutes * 60),
    },
    "aggregate-usage-every-minute": {
        "task": "nexora.aggregate_usage",
        "schedule": 60.0,
    },
    "housekeeping-hourly": {
        "task": "nexora.cleanup_housekeeping",
        "schedule": 3600.0,
    },
}


def _request_metadata(task) -> tuple[str | None, str | None]:
    request = getattr(task, "request", None)
    if request is None:
        return None, None
    return getattr(request, "id", None), getattr(request, "hostname", None)


@app.task(name="nexora.healthcheck")
def healthcheck() -> dict[str, str]:
    return {"status": "ok", "service": "celery-worker"}


@app.task(
    bind=True,
    name="nexora.sync_model_catalog",
    max_retries=2,
    default_retry_delay=10,
)
def sync_model_catalog_task(self) -> dict:
    task_id, worker_name = _request_metadata(self)
    try:
        with JobRun(
            settings.database_url,
            "nexora.sync_model_catalog",
            task_id=task_id,
            worker_name=worker_name,
        ) as run:
            result = sync_model_catalog(settings)
            run.result = result
            return result
    except Exception as exc:
        countdown = min(60, 10 * (2 ** int(getattr(self.request, "retries", 0))))
        raise self.retry(exc=exc, countdown=countdown)


@app.task(
    bind=True,
    name="nexora.aggregate_usage",
    max_retries=1,
    default_retry_delay=10,
)
def aggregate_usage_task(self) -> dict:
    task_id, worker_name = _request_metadata(self)
    try:
        with JobRun(
            settings.database_url,
            "nexora.aggregate_usage",
            task_id=task_id,
            worker_name=worker_name,
        ) as run:
            result = aggregate_usage(settings)
            run.result = result
            return result
    except Exception as exc:
        raise self.retry(exc=exc, countdown=10)


@app.task(bind=True, name="nexora.cleanup_housekeeping")
def cleanup_housekeeping_task(self) -> dict:
    task_id, worker_name = _request_metadata(self)
    with JobRun(
        settings.database_url,
        "nexora.cleanup_housekeeping",
        task_id=task_id,
        worker_name=worker_name,
    ) as run:
        result = cleanup_housekeeping(settings)
        run.result = result
        return result


# Backward-compatible task name for operators/scripts from earlier alpha builds.
@app.task(name="nexora.cleanup_idempotency")
def cleanup_idempotency_compat() -> dict:
    return cleanup_housekeeping(settings)


@worker_ready.connect
def schedule_initial_catalog_sync(sender=None, **_kwargs) -> None:
    # Do not wait for the first periodic interval after a worker restart.
    # Advisory locking in the sync task makes duplicate startup signals safe.
    if sender is not None:
        sender.app.send_task("nexora.sync_model_catalog")
