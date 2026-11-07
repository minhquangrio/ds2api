FROM python:3.13-slim

WORKDIR /app

ENV PYTHONUNBUFFERED=1 \
    DEBIAN_FRONTEND=noninteractive \
    MUSE_WORKER_DRIVER=engine

RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY workers/muse/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt \
    && playwright install --with-deps chromium

COPY workers/muse /app/workers/muse

EXPOSE 8765

CMD ["python", "-m", "workers.muse.server", "--host", "0.0.0.0", "--port", "8765"]
