"""Environment-driven settings for the embedder service.

All knobs are sourced from env vars so the same image runs locally and in docker
without rebuilds. Defaults match `.env.example` and `docker-compose.yml`.
"""

from __future__ import annotations

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    # Embedding model. `bge-m3` is an alias users set via the Web settings page;
    # we map it to the actual HF id so the OpenAI-compatible API can stay stable.
    embed_model: str = "bge-m3"

    # Qdrant
    qdrant_url: str = "http://qdrant:6333"
    qdrant_collection: str = "knowlib_notes"
    qdrant_api_key: str | None = None

    # Vault layout (paths inside the container)
    vault_path: str = "/data/vault"
    knowlib_dir: str = ".knowlib"

    # Chunking knobs (kept here so ops can tune without touching code)
    chunk_max_tokens: int = 800
    chunk_overlap_tokens: int = 80

    # Watcher debounce: editor saves emit a flurry of events; we coalesce them
    # so a single user save does not produce N redundant embeddings.
    watch_debounce_ms: int = 500

    # Cache locations honoured by HF / sentence-transformers; the docker volume
    # mounts `./data/embedder-cache` here so model weights survive restarts.
    hf_home: str = "/cache/huggingface"
    sentence_transformers_home: str = "/cache/sentence-transformers"

    # Server
    host: str = "0.0.0.0"
    port: int = 8000

    # Startup retry budget for Qdrant. Compose brings services up in parallel,
    # so we expect a few connection refusals before Qdrant is ready.
    qdrant_startup_timeout_s: int = 60

    model_config = SettingsConfigDict(env_file=".env", extra="ignore")


# Map user-facing aliases to actual HuggingFace model ids. Users typing "bge-m3"
# in the Web settings page should not need to know about the org prefix.
MODEL_ALIASES: dict[str, str] = {
    "bge-m3": "BAAI/bge-m3",
    "bge-large-zh": "BAAI/bge-large-zh-v1.5",
    "bge-small-en": "BAAI/bge-small-en-v1.5",
}


def resolve_model_id(name: str) -> str:
    """Resolve an alias to a HuggingFace model id, or pass through if already an id."""
    return MODEL_ALIASES.get(name, name)


settings = Settings()
