"""FastAPI entrypoint for the embedder service.

Responsibilities:
  - Start watchdog after Qdrant + collection are ready.
  - On startup, run a full reconcile so new files added while we were down get
    picked up, and orphans (deleted while we were down) get removed.
  - Expose:
      POST /v1/embeddings   OpenAI-compatible (used by knowlib-api + tidy)
      GET  /healthz         service status + counts
      POST /reindex         full re-scan (admin trigger)
"""

from __future__ import annotations

import asyncio
import logging
import os
from contextlib import asynccontextmanager
from typing import Any

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

from .config import settings
from .embed import get_model, to_openai_response
from .indexer import index_file, reconcile
from .qdrant_store import QStore
from .vault_watcher import VaultWatcher

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
)
log = logging.getLogger("embedder")


class _State:
    store: QStore | None = None
    watcher: VaultWatcher | None = None
    last_reconcile: dict[str, int] | None = None


state = _State()


def _on_upsert(rel_path: str) -> None:
    if state.store is None:
        return
    try:
        index_file(rel_path, state.store, get_model())
    except Exception:  # noqa: BLE001
        log.exception("upsert failed: %s", rel_path)


def _on_delete(rel_path: str) -> None:
    if state.store is None:
        return
    try:
        state.store.delete_file(rel_path)
        log.info("deleted %s", rel_path)
    except Exception:  # noqa: BLE001
        log.exception("delete failed: %s", rel_path)


@asynccontextmanager
async def lifespan(app: FastAPI):  # noqa: D401
    # HF cache dirs respect env vars; set them defensively in case the user
    # didn't pass them through compose.
    os.environ.setdefault("HF_HOME", settings.hf_home)
    os.environ.setdefault("SENTENCE_TRANSFORMERS_HOME", settings.sentence_transformers_home)

    store = QStore()
    log.info("waiting for qdrant at %s", settings.qdrant_url)
    await asyncio.to_thread(store.wait_ready)

    model = get_model()
    # Touch the model in a worker thread so the FastAPI startup probe sees a
    # responsive server while bge-m3 weights download (first run can be 1-2GB).
    log.info("loading model %s", model.name)
    dim = await asyncio.to_thread(lambda: model.dim)
    store.ensure_collection(vector_size=dim)
    log.info("collection %s ready (dim=%d)", store.collection, dim)

    state.store = store

    log.info("running startup reconcile")
    state.last_reconcile = await asyncio.to_thread(reconcile, store, model)
    log.info("reconcile stats: %s", state.last_reconcile)

    state.watcher = VaultWatcher(on_upsert=_on_upsert, on_delete=_on_delete)
    state.watcher.start()

    try:
        yield
    finally:
        if state.watcher:
            state.watcher.stop()


app = FastAPI(title="knowLib embedder", version="0.1.0", lifespan=lifespan)


# === OpenAI-compatible /v1/embeddings ===========================================


class EmbeddingsRequest(BaseModel):
    model: str | None = None
    input: str | list[str]


@app.post("/v1/embeddings")
async def embeddings(req: EmbeddingsRequest) -> dict[str, Any]:
    inputs = [req.input] if isinstance(req.input, str) else list(req.input)
    if not inputs:
        raise HTTPException(status_code=400, detail="input must be non-empty")
    if any(not isinstance(s, str) for s in inputs):
        raise HTTPException(status_code=400, detail="input items must be strings")

    model = get_model()
    vectors = await asyncio.to_thread(model.encode, inputs)
    return to_openai_response(req.model or model.name, vectors, inputs)


# === Health =====================================================================


@app.get("/healthz")
def healthz() -> dict[str, Any]:
    qdrant_ok = False
    indexed = 0
    if state.store is not None:
        try:
            indexed = len(state.store.all_indexed_paths())
            qdrant_ok = True
        except Exception:  # noqa: BLE001
            qdrant_ok = False
    return {
        "ok": qdrant_ok,
        "model": get_model().name,
        "indexed_files": indexed,
        "qdrant_url": settings.qdrant_url,
        "vault_path": settings.vault_path,
        "last_reconcile": state.last_reconcile,
    }


# === Admin: full reindex (no auth — only reachable on docker network) ===========


@app.post("/reindex")
async def reindex_all() -> dict[str, Any]:
    if state.store is None:
        raise HTTPException(status_code=503, detail="qdrant not ready yet")
    stats = await asyncio.to_thread(reconcile, state.store, get_model())
    state.last_reconcile = stats
    return stats
