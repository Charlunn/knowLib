"""Embedding model wrapper + OpenAI-compatible /v1/embeddings response shape.

Why a wrapper:
  - Lazy-loads the model on first use so the FastAPI server can start fast and
    docker compose health-checks don't time out while bge-m3 weights download.
  - Normalizes embeddings (L2) so Qdrant cosine search behaves like dot product.
  - Returns the OpenAI-compatible payload shape so other services can call us
    via their existing OpenAI client (no separate code path for "internal" embeds).
  - Serialises encode() calls behind a thread lock so concurrent requests don't
    trample sentence-transformers' internal state.
"""

from __future__ import annotations

import threading
import time
from typing import Iterable

from sentence_transformers import SentenceTransformer

from .config import resolve_model_id, settings


class EmbedModel:
    def __init__(self, name: str | None = None):
        self._name = name or settings.embed_model
        self._model: SentenceTransformer | None = None
        self._load_lock = threading.Lock()
        # Bounded semaphore protects encode() against unbounded concurrent
        # callers. With sentence-transformers + torch, two threads racing into
        # the same model produce nondeterministic output and OOMs.
        self._encode_sem = threading.BoundedSemaphore(max(1, settings.embed_concurrency))

    @property
    def name(self) -> str:
        return self._name

    def _ensure_loaded(self) -> SentenceTransformer:
        # Double-checked locking: cheap fast path once warm.
        if self._model is not None:
            return self._model
        with self._load_lock:
            if self._model is None:
                model_id = resolve_model_id(self._name)
                # cache_folder picks up SENTENCE_TRANSFORMERS_HOME from env too,
                # but pass explicitly so behavior is obvious.
                self._model = SentenceTransformer(
                    model_id,
                    cache_folder=settings.sentence_transformers_home,
                )
            return self._model

    def encode(self, texts: list[str]) -> list[list[float]]:
        if not texts:
            return []
        m = self._ensure_loaded()
        # Serialise encode() — concurrent calls into one SentenceTransformer
        # are unsafe in practice. asyncio.to_thread fans out callers across
        # the threadpool; this semaphore brings them back to a controlled
        # number of in-flight calls.
        with self._encode_sem:
            # normalize_embeddings=True so we can use Cosine OR Dot in Qdrant
            # interchangeably. Cosine on normalized vectors == dot product.
            vecs = m.encode(
                texts,
                normalize_embeddings=True,
                convert_to_numpy=True,
                show_progress_bar=False,
            )
        return [v.tolist() for v in vecs]

    @property
    def dim(self) -> int:
        return self._ensure_loaded().get_sentence_embedding_dimension()


_singleton: EmbedModel | None = None


def get_model() -> EmbedModel:
    global _singleton
    if _singleton is None:
        _singleton = EmbedModel()
    return _singleton


def to_openai_response(model_name: str, vectors: list[list[float]], inputs: Iterable[str]) -> dict:
    """Shape the response like OpenAI's /v1/embeddings so generic clients work."""
    input_list = list(inputs)
    total_chars = sum(len(s) for s in input_list)
    return {
        "object": "list",
        "data": [
            {"object": "embedding", "index": i, "embedding": v}
            for i, v in enumerate(vectors)
        ],
        "model": model_name,
        # OpenAI reports prompt_tokens; we don't run a tokenizer here so use a
        # rough char-based proxy. Clients that care about exact counts will run
        # their own tokenizer anyway.
        "usage": {"prompt_tokens": total_chars // 4, "total_tokens": total_chars // 4},
        "_generated_at": int(time.time()),
    }
