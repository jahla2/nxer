from contextlib import contextmanager
from typing import Iterator

import psycopg

from nexora_control.config import get_settings


@contextmanager
def get_connection() -> Iterator[psycopg.Connection]:
    settings = get_settings()
    with psycopg.connect(settings.database_url) as connection:
        yield connection
