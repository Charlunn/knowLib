# syntax=docker/dockerfile:1.6
# CPU-only torch; kept lean so the final image stays under ~3GB even with the
# bge-m3 weights downloaded on first run.
FROM python:3.11-slim AS base

ENV PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    HF_HOME=/cache/huggingface \
    SENTENCE_TRANSFORMERS_HOME=/cache/sentence-transformers \
    TRANSFORMERS_NO_ADVISORY_WARNINGS=1

# build deps for sentence-transformers / tokenizers wheels on slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Install CPU-only torch first so subsequent pip resolves don't pull CUDA.
COPY requirements.txt ./
RUN pip install --extra-index-url https://download.pytorch.org/whl/cpu -r requirements.txt

# Copy the package as a directory so `python -m embedder.main` works.
COPY . /app/embedder
ENV PYTHONPATH=/app

EXPOSE 8000

# uvicorn binds 0.0.0.0:8000 by default; --proxy-headers because we sit behind Caddy.
CMD ["uvicorn", "embedder.main:app", "--host", "0.0.0.0", "--port", "8000", "--proxy-headers"]
