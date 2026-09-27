FROM python:3.13-slim AS build

ENV VIRTUAL_ENV=/opt/venv
ENV PATH="$VIRTUAL_ENV/bin:$PATH"

WORKDIR /build
RUN python -m venv "$VIRTUAL_ENV"     && pip install --no-cache-dir --upgrade pip setuptools wheel

COPY pyproject.toml ./
COPY src ./src
RUN pip install --no-cache-dir .

FROM python:3.13-slim

ENV VIRTUAL_ENV=/opt/venv
ENV PATH="$VIRTUAL_ENV/bin:$PATH"
ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1

RUN addgroup --system nexora     && adduser --system --ingroup nexora --home /nonexistent --no-create-home nexora

COPY --from=build /opt/venv /opt/venv

USER nexora
EXPOSE 8001

CMD ["uvicorn", "nexora_control.main:app", "--host", "0.0.0.0", "--port", "8001", "--workers", "2", "--proxy-headers", "--forwarded-allow-ips=*"]
