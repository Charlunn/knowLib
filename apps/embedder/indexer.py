"""Index a single markdown file from disk into Qdrant.

Frontmatter is parsed once here so the chunker only sees the body, and so
title/category/tags can ride along on every chunk's payload (Qdrant filter
queries hit those without re-reading the file).
"""

from __future__ import annotations

import logging
import os
import time
from pathlib import Path
from typing import Any

import frontmatter

from .chunker import chunk_markdown
from .config import settings
from .embed import EmbedModel
from .qdrant_store import QStore, file_sha256

log = logging.getLogger("indexer")


def _read_note(rel_path: str) -> tuple[dict[str, Any], str, str] | None:
    """Read (frontmatter_dict, body, content_sha256) for a vault-relative path.

    Returns None if the file is missing or unreadable.
    """
    abs_path = Path(settings.vault_path) / rel_path
    try:
        raw = abs_path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError):
        return None
    try:
        post = frontmatter.loads(raw)
        meta: dict[str, Any] = dict(post.metadata or {})
        body = post.content or ""
    except Exception:  # noqa: BLE001
        # frontmatter parse failure: treat the whole file as body, no meta.
        meta = {}
        body = raw
    return meta, body, file_sha256(raw)


def _derive_title(meta: dict[str, Any], rel_path: str, body: str) -> str:
    if isinstance(meta.get("title"), str) and meta["title"].strip():
        return meta["title"].strip()
    # Fall back to the first H1 / first non-empty line / filename stem.
    for line in body.splitlines():
        s = line.strip()
        if s.startswith("# "):
            return s.lstrip("#").strip()
        if s:
            return s[:60]
    return Path(rel_path).stem


def _derive_category(meta: dict[str, Any], rel_path: str) -> str:
    if isinstance(meta.get("category"), str) and meta["category"].strip():
        return meta["category"].strip()
    # Synthesize from path: notes/学习/高数/微分方程.md -> "学习/高数"
    parts = rel_path.split("/")
    if len(parts) >= 2 and parts[0] in {"notes", "inbox", "atlas"}:
        return "/".join(parts[1:-1]) or parts[0]
    return "/".join(parts[:-1]) or ""


def _derive_tags(meta: dict[str, Any]) -> list[str]:
    tags = meta.get("tags")
    if isinstance(tags, list):
        return [str(t) for t in tags if t]
    if isinstance(tags, str):
        return [t.strip() for t in tags.split(",") if t.strip()]
    return []


def index_file(
    rel_path: str,
    store: QStore,
    model: EmbedModel,
    *,
    known_sha: str | None = None,
    known_sha_loaded: bool = False,
) -> str:
    """Embed and upsert a single file.

    Returns one of: "skipped" (sha unchanged), "indexed", "deleted", "missing".

    `known_sha` and `known_sha_loaded`: when reconcile() has already pre-fetched
    SHAs for every indexed file in one batch query, it passes them in here so
    we don't pay an extra Qdrant round-trip per file. `known_sha_loaded=True`
    with `known_sha=None` means "we checked, this file is not in Qdrant yet".
    """
    parsed = _read_note(rel_path)
    if parsed is None:
        # File vanished — make sure no stale points remain.
        store.delete_file(rel_path)
        return "missing"

    meta, body, sha = parsed
    if known_sha_loaded:
        existing_sha = known_sha
    else:
        existing_sha = store.get_file_sha(rel_path)
    if existing_sha == sha:
        return "skipped"

    chunks = chunk_markdown(
        body,
        max_tokens=settings.chunk_max_tokens,
        overlap_tokens=settings.chunk_overlap_tokens,
    )
    if not chunks:
        # Empty / whitespace-only file: no chunks, just clean up any prior state.
        store.delete_file(rel_path)
        return "indexed"

    vectors = model.encode([c.content for c in chunks])

    common = {
        "path": rel_path,
        "title": _derive_title(meta, rel_path, body),
        "category": _derive_category(meta, rel_path),
        "tags": _derive_tags(meta),
        "content_sha256": sha,
        "updated_at": int(time.time()),
    }
    chunk_payloads = [
        {
            "heading_path": c.heading_path,
        }
        for c in chunks
    ]
    store.upsert_file(
        path=rel_path,
        chunks=[c.content for c in chunks],
        vectors=vectors,
        common_payload=common,
        chunk_payloads=chunk_payloads,
    )
    log.info("indexed %s (%d chunks)", rel_path, len(chunks))
    return "indexed"


def list_vault_md_files() -> list[str]:
    """Walk the vault and return all indexable .md paths (relative, slash-separated)."""
    out: list[str] = []
    root = Path(settings.vault_path)
    if not root.exists():
        return out
    for dirpath, dirnames, filenames in os.walk(root):
        # In-place prune of hidden dirs and KNOWLIB_DIR so we never descend.
        dirnames[:] = [
            d for d in dirnames
            if not d.startswith(".") and d != settings.knowlib_dir
        ]
        for fn in filenames:
            if not fn.endswith(".md") or fn.startswith("."):
                continue
            abs_p = os.path.join(dirpath, fn)
            rel = os.path.relpath(abs_p, root).replace(os.sep, "/")
            out.append(rel)
    return out


def reconcile(store: QStore, model: EmbedModel) -> dict[str, int]:
    """Full sync: re-index changed files, drop orphans whose files vanished.

    Stats useful for /healthz and ops debugging.

    We pull every indexed (path, sha) pair from Qdrant in a single paginated
    scroll BEFORE walking the disk, then index_file() consults the in-memory
    map instead of issuing a per-file query. This turns an O(N) round-trip
    pattern into O(N/512), which matters once the vault has thousands of files.
    """
    on_disk = set(list_vault_md_files())
    indexed_shas = store.all_file_shas()
    in_qdrant = set(indexed_shas.keys())

    stats = {"indexed": 0, "skipped": 0, "deleted": 0, "missing": 0}

    for rel in sorted(on_disk):
        outcome = index_file(
            rel,
            store,
            model,
            known_sha=indexed_shas.get(rel),
            known_sha_loaded=True,
        )
        stats[outcome] = stats.get(outcome, 0) + 1

    for orphan in in_qdrant - on_disk:
        store.delete_file(orphan)
        stats["deleted"] = stats.get("deleted", 0) + 1
        log.info("removed orphan %s", orphan)

    return stats
