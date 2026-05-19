"""Qdrant client wrapper.

We treat the vault file path (forward-slashed, relative to VAULT_PATH) as the
canonical key. Each markdown file is split into N chunks; each chunk becomes
one Qdrant point with payload[path] set to the file path. To delete or update
a file we filter on payload[path] == path and recreate. content_sha256 in the
payload lets the reconciler skip files that haven't changed since last index.
"""

from __future__ import annotations

import hashlib
import time
import uuid
from typing import Any

from qdrant_client import QdrantClient
from qdrant_client.http import models as qm

from .config import settings


def _stable_point_id(path: str, chunk_index: int) -> str:
    """Deterministic UUIDv5 from (path, chunk_index) so re-index is idempotent."""
    return str(uuid.uuid5(uuid.NAMESPACE_URL, f"knowlib::{path}::{chunk_index}"))


def file_sha256(content: str) -> str:
    return hashlib.sha256(content.encode("utf-8")).hexdigest()


class QStore:
    def __init__(self, url: str | None = None, collection: str | None = None):
        self._url = url or settings.qdrant_url
        self._collection = collection or settings.qdrant_collection
        self._client = QdrantClient(url=self._url, api_key=settings.qdrant_api_key)

    @property
    def collection(self) -> str:
        return self._collection

    def wait_ready(self, timeout_s: int | None = None) -> None:
        """Block until Qdrant answers, or raise after `timeout_s`.

        Compose brings Qdrant up in parallel with us, so a few connection
        refusals at startup are normal — exponential-ish backoff with a cap.
        """
        deadline = time.time() + (timeout_s or settings.qdrant_startup_timeout_s)
        delay = 0.5
        while True:
            try:
                self._client.get_collections()
                return
            except Exception:
                if time.time() > deadline:
                    raise
                time.sleep(delay)
                delay = min(delay * 1.5, 5.0)

    def ensure_collection(self, vector_size: int) -> None:
        """Create the collection on first run; no-op afterwards.

        We pick Cosine because we normalize embeddings on write (see embed.py),
        which makes cosine == dot product but keeps the metric name conventional.
        """
        existing = {c.name for c in self._client.get_collections().collections}
        if self._collection in existing:
            return
        self._client.create_collection(
            collection_name=self._collection,
            vectors_config=qm.VectorParams(size=vector_size, distance=qm.Distance.COSINE),
        )
        # Index payload[path] for cheap "delete by path" / "list by file" queries.
        self._client.create_payload_index(
            collection_name=self._collection,
            field_name="path",
            field_schema=qm.PayloadSchemaType.KEYWORD,
        )
        self._client.create_payload_index(
            collection_name=self._collection,
            field_name="category",
            field_schema=qm.PayloadSchemaType.KEYWORD,
        )

    def get_file_sha(self, path: str) -> str | None:
        """Return the content_sha256 stored for the FIRST chunk of `path`, or None."""
        res, _ = self._client.scroll(
            collection_name=self._collection,
            scroll_filter=qm.Filter(must=[qm.FieldCondition(key="path", match=qm.MatchValue(value=path))]),
            limit=1,
            with_payload=True,
            with_vectors=False,
        )
        if not res:
            return None
        return res[0].payload.get("content_sha256")

    def all_indexed_paths(self) -> set[str]:
        """Enumerate every path currently in the collection (for reconcile)."""
        paths: set[str] = set()
        offset = None
        while True:
            res, offset = self._client.scroll(
                collection_name=self._collection,
                limit=512,
                offset=offset,
                with_payload=["path"],
                with_vectors=False,
            )
            for p in res:
                if p.payload and "path" in p.payload:
                    paths.add(p.payload["path"])
            if offset is None:
                break
        return paths

    def delete_file(self, path: str) -> None:
        self._client.delete(
            collection_name=self._collection,
            points_selector=qm.FilterSelector(
                filter=qm.Filter(
                    must=[qm.FieldCondition(key="path", match=qm.MatchValue(value=path))]
                )
            ),
        )

    def upsert_file(
        self,
        *,
        path: str,
        chunks: list[str],
        vectors: list[list[float]],
        common_payload: dict[str, Any],
        chunk_payloads: list[dict[str, Any]],
    ) -> None:
        assert len(chunks) == len(vectors) == len(chunk_payloads)

        # Wipe any existing points for this path first so chunk-count shrinks
        # don't leave orphans, then upsert. Two roundtrips, but safe and simple.
        self.delete_file(path)

        points = []
        for i, (text, vec, extra) in enumerate(zip(chunks, vectors, chunk_payloads)):
            payload = {**common_payload, **extra, "content": text, "chunk_index": i, "path": path}
            points.append(
                qm.PointStruct(
                    id=_stable_point_id(path, i),
                    vector=vec,
                    payload=payload,
                )
            )
        self._client.upsert(collection_name=self._collection, points=points, wait=True)

    def search(self, vector: list[float], k: int = 10, category_prefix: str | None = None) -> list[dict[str, Any]]:
        flt = None
        if category_prefix:
            flt = qm.Filter(
                must=[qm.FieldCondition(key="category", match=qm.MatchText(text=category_prefix))]
            )
        hits = self._client.search(
            collection_name=self._collection,
            query_vector=vector,
            query_filter=flt,
            limit=k,
            with_payload=True,
        )
        return [
            {
                "id": str(h.id),
                "score": h.score,
                "payload": h.payload or {},
            }
            for h in hits
        ]
