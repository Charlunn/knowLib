"""watchdog handler -> debounced queue -> background indexing worker.

The editor save problem: most editors write a temp file then rename, which
triggers a FileCreatedEvent + FileModifiedEvent + maybe FileDeletedEvent for
the temp. Without debouncing we'd embed each file three or four times per save.
We coalesce events per path inside a window (settings.watch_debounce_ms) and
only process the latest state.

We also skip:
  - Anything under KNOWLIB_DIR (local-only state)
  - Hidden / dotfiles
  - Non-.md files
"""

from __future__ import annotations

import asyncio
import logging
import os
import threading
import time
from pathlib import Path
from typing import Callable

from watchdog.events import FileSystemEvent, FileSystemEventHandler
from watchdog.observers import Observer

from .config import settings

log = logging.getLogger("vault_watcher")


def _is_indexable(rel_path: str) -> bool:
    if not rel_path.endswith(".md"):
        return False
    parts = rel_path.split("/")
    if any(p.startswith(".") for p in parts):  # excludes .knowlib/, .obsidian/, etc.
        return False
    return True


def _to_rel(abs_path: str) -> str | None:
    """Convert an absolute path under VAULT_PATH to a forward-slash relative path."""
    try:
        rel = os.path.relpath(abs_path, settings.vault_path)
    except ValueError:
        return None
    rel = rel.replace(os.sep, "/")
    if rel.startswith(".."):
        return None
    return rel


class _Handler(FileSystemEventHandler):
    def __init__(self, enqueue: Callable[[str, str], None]):
        self._enqueue = enqueue

    def on_any_event(self, event: FileSystemEvent) -> None:  # noqa: D401
        if event.is_directory:
            return
        # event.event_type ∈ {"created","modified","deleted","moved"}
        if event.event_type == "moved":
            # treat as delete(src) + upsert(dest)
            src = _to_rel(event.src_path)
            dest = _to_rel(event.dest_path)
            if src and _is_indexable(src):
                self._enqueue(src, "deleted")
            if dest and _is_indexable(dest):
                self._enqueue(dest, "modified")
            return

        rel = _to_rel(event.src_path)
        if rel is None or not _is_indexable(rel):
            return
        self._enqueue(rel, event.event_type)


class VaultWatcher:
    """Owns the Observer and the debounce buffer.

    Producers (the watchdog handler) push (path, kind) into a dict keyed by
    path. A consumer task wakes up every debounce window and drains paths
    whose last-seen timestamp is older than the window — i.e. the editor
    has stopped fiddling with that file.
    """

    def __init__(
        self,
        on_upsert: Callable[[str], None],
        on_delete: Callable[[str], None],
    ):
        self._on_upsert = on_upsert
        self._on_delete = on_delete
        self._observer = Observer()
        self._lock = threading.Lock()
        self._pending: dict[str, tuple[float, str]] = {}  # path -> (last_ts, kind)
        self._stop = threading.Event()

    def _enqueue(self, path: str, kind: str) -> None:
        with self._lock:
            # delete supersedes everything; otherwise newest wins
            prev = self._pending.get(path)
            if prev and prev[1] == "deleted" and kind != "deleted":
                # Rare: file deleted then recreated quickly. Treat as upsert.
                self._pending[path] = (time.time(), "modified")
            else:
                self._pending[path] = (time.time(), kind)

    def _drain_loop(self) -> None:
        window_s = settings.watch_debounce_ms / 1000.0
        while not self._stop.is_set():
            now = time.time()
            ready: list[tuple[str, str]] = []
            with self._lock:
                stale_keys = [
                    p for p, (ts, _) in self._pending.items() if now - ts >= window_s
                ]
                for p in stale_keys:
                    ready.append((p, self._pending.pop(p)[1]))
            for path, kind in ready:
                try:
                    if kind == "deleted":
                        self._on_delete(path)
                    else:
                        # If the file vanished between event and drain, fall back to delete.
                        abs_p = Path(settings.vault_path) / path
                        if abs_p.exists():
                            self._on_upsert(path)
                        else:
                            self._on_delete(path)
                except Exception:  # noqa: BLE001
                    log.exception("indexing failed for %s (%s)", path, kind)
            self._stop.wait(window_s / 2 if window_s > 0 else 0.25)

    def start(self) -> None:
        os.makedirs(settings.vault_path, exist_ok=True)
        self._observer.schedule(_Handler(self._enqueue), settings.vault_path, recursive=True)
        self._observer.start()
        threading.Thread(target=self._drain_loop, daemon=True, name="vault-drain").start()
        log.info("vault watcher started on %s", settings.vault_path)

    def stop(self) -> None:
        self._stop.set()
        self._observer.stop()
        self._observer.join(timeout=5)


# Async helper for the FastAPI lifespan: wraps the threaded watcher in something
# the event loop can await on shutdown.
async def run_watcher(
    on_upsert: Callable[[str], None],
    on_delete: Callable[[str], None],
) -> VaultWatcher:
    watcher = VaultWatcher(on_upsert, on_delete)
    watcher.start()
    return watcher


def shutdown(watcher: VaultWatcher) -> None:
    watcher.stop()


# Keep asyncio importable but only used by main.py — avoids a hard dep.
_ = asyncio
