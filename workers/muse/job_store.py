"""Durable media blob store and video job registry for the Muse worker.

The worker owns media state: Go proxies requests, but task identity, caller
ownership and bytes live here so a video job keeps working across worker
restarts and polling requests. Records are plain JSON plus byte files written
atomically; nothing requires a database or network access.
"""

from __future__ import annotations

import json
import logging
import os
import time
import uuid
from pathlib import Path
from typing import Dict, List, Optional

from .media_protocol import VideoTask

logger = logging.getLogger("muse_worker.jobs")

TERMINAL_STATUSES = {"completed", "failed", "interrupted"}

# Statuses a job can be in. `interrupted` is set on restart for jobs that were
# still running when the worker stopped; they are never silently re-run because
# upstream may already have billed the generation.
STATUS_QUEUED = "queued"
STATUS_PROCESSING = "processing"
STATUS_COMPLETED = "completed"
STATUS_FAILED = "failed"
STATUS_INTERRUPTED = "interrupted"


def default_media_dir() -> Path:
    raw = os.getenv("MUSE_MEDIA_DIR", "").strip()
    if raw:
        return Path(raw).expanduser()
    return Path(__file__).resolve().parents[2] / "data" / "muse_media"


def ttl_seconds() -> int:
    try:
        hours = int(os.getenv("MUSE_MEDIA_TTL_HOURS", "24"))
    except ValueError:
        hours = 24
    return max(60, hours * 3600)


def max_blob_bytes() -> int:
    try:
        value = int(os.getenv("MUSE_MEDIA_MAX_BYTES", str(64 << 20)))
    except ValueError:
        value = 64 << 20
    return max(1024, value)


class MediaOverflow(Exception):
    """Raised when generated media exceeds the configured size limit."""


class BlobStore:
    """Content-addressed-ish byte store with TTL purge and an index file."""

    def __init__(self, root: Optional[Path] = None) -> None:
        self.root = Path(root) if root is not None else default_media_dir()
        self.blob_dir = self.root / "blobs"
        self.index_path = self.root / "blobs.json"
        self._index: Dict[str, Dict[str, object]] = {}
        self._load()

    def _load(self) -> None:
        try:
            if self.index_path.exists():
                data = json.loads(self.index_path.read_text(encoding="utf-8"))
                if isinstance(data, dict):
                    self._index = {str(k): dict(v) for k, v in data.items()}
        except Exception as exc:
            logger.warning("media index unreadable, starting empty (%s)", type(exc).__name__)
            self._index = {}

    def _save(self) -> None:
        self._write_atomic(self.index_path, json.dumps(self._index, ensure_ascii=False))

    @staticmethod
    def _write_atomic(path: Path, text: str) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        tmp = path.with_name(path.name + f".{uuid.uuid4().hex[:8]}.tmp")
        tmp.write_text(text, encoding="utf-8")
        os.replace(tmp, path)

    def store(self, data: bytes, content_type: str) -> str:
        if len(data) > max_blob_bytes():
            raise MediaOverflow(f"media size {len(data)} exceeds limit {max_blob_bytes()}")
        media_id = f"med_{uuid.uuid4().hex[:16]}"
        self.blob_dir.mkdir(parents=True, exist_ok=True)
        target = self.blob_dir / f"{media_id}.bin"
        tmp = target.with_suffix(".tmp")
        tmp.write_bytes(data)
        os.replace(tmp, target)
        self._index[media_id] = {
            "content_type": content_type,
            "size": len(data),
            "created_at": time.time(),
        }
        self._save()
        return media_id

    def read(self, media_id: str) -> Optional[bytes]:
        entry = self._index.get(media_id)
        if not entry:
            return None
        target = self.blob_dir / f"{media_id}.bin"
        if not target.exists():
            return None
        return target.read_bytes()

    def content_type(self, media_id: str) -> str:
        entry = self._index.get(media_id) or {}
        return str(entry.get("content_type") or "application/octet-stream")

    def purge_expired(self) -> int:
        deadline = time.time() - ttl_seconds()
        removed = 0
        for media_id in list(self._index):
            if float(self._index[media_id].get("created_at") or 0) < deadline:
                self._discard(media_id)
                removed += 1
        if removed:
            self._save()
        return removed

    def discard(self, media_id: str) -> None:
        if media_id in self._index:
            self._discard(media_id)
            self._save()

    def _discard(self, media_id: str) -> None:
        self._index.pop(media_id, None)
        target = self.blob_dir / f"{media_id}.bin"
        try:
            if target.exists():
                target.unlink()
        except OSError as exc:
            logger.warning("media blob cleanup failed for %s: %s", media_id, exc)


class JobRegistry:
    """Durable video job records plus in-process task bookkeeping."""

    def __init__(self, root: Optional[Path] = None, blobs: Optional[BlobStore] = None) -> None:
        self.root = Path(root) if root is not None else default_media_dir()
        self.job_dir = self.root / "jobs"
        self.blobs = blobs if blobs is not None else BlobStore(self.root)
        self._tasks: Dict[str, VideoTask] = {}
        self._loads: Dict[str, object] = {}
        self._load_all()

    def _path(self, task_id: str) -> Path:
        return self.job_dir / f"{task_id}.json"

    def _load_all(self) -> None:
        try:
            files: List[Path] = sorted(self.job_dir.glob("*.json")) if self.job_dir.exists() else []
        except OSError as exc:
            logger.warning("job directory unreadable: %s", exc)
            return
        for path in files:
            try:
                task = VideoTask.from_dict(json.loads(path.read_text(encoding="utf-8")))
            except Exception as exc:
                logger.warning("job record %s unreadable (%s)", path.name, type(exc).__name__)
                continue
            if not task.id:
                continue
            if task.status not in TERMINAL_STATUSES:
                # A worker restart never resumes an in-flight browser job; the
                # generation may already exist upstream, so surface it as
                # interrupted instead of pretending it is still queued.
                task.status = STATUS_INTERRUPTED
                task.stage = "interrupted"
                task.error = "worker restarted while the job was running"
                task.error_code = "job_interrupted"
                task.updated_at = time.time()
                self._tasks[task.id] = task
                self._persist(task)
            else:
                self._tasks[task.id] = task

    def _persist(self, task: VideoTask) -> None:
        try:
            self.job_dir.mkdir(parents=True, exist_ok=True)
            tmp = self._path(task.id).with_suffix(".tmp")
            tmp.write_text(json.dumps(task.to_dict(), ensure_ascii=False), encoding="utf-8")
            os.replace(tmp, self._path(task.id))
        except OSError as exc:
            logger.warning("job record %s could not be persisted: %s", task.id, exc)

    def create(self, task: VideoTask) -> VideoTask:
        now = time.time()
        if not task.id:
            task.id = f"vid_{uuid.uuid4().hex[:16]}"
        task.created_at = task.created_at or now
        task.updated_at = now
        task.status = STATUS_QUEUED
        task.stage = STATUS_QUEUED
        self._tasks[task.id] = task
        self._persist(task)
        return task

    def update(self, task: VideoTask) -> VideoTask:
        task.updated_at = time.time()
        self._tasks[task.id] = task
        self._persist(task)
        return task

    def get(self, task_id: str, caller_id: str = "") -> Optional[VideoTask]:
        task = self._tasks.get(task_id)
        if task is None:
            return None
        if caller_id and task.caller_id and task.caller_id != caller_id:
            return None
        return task

    def register_handle(self, task_id: str, handle: object) -> None:
        self._loads[task_id] = handle

    def forget_handle(self, task_id: str) -> None:
        self._loads.pop(task_id, None)

    def has_handle(self, task_id: str) -> bool:
        return task_id in self._loads

    def list(self, caller_id: str, limit: int = 50) -> List[VideoTask]:
        items = [t for t in self._tasks.values() if not caller_id or t.caller_id == caller_id]
        items.sort(key=lambda t: t.created_at, reverse=True)
        return items[: max(1, limit)]

    def purge_expired(self) -> int:
        deadline = time.time() - ttl_seconds()
        removed = 0
        for task_id, task in list(self._tasks.items()):
            if task.status in TERMINAL_STATUSES and task.updated_at < deadline and not self.has_handle(task_id):
                self._tasks.pop(task_id, None)
                self._loads.pop(task_id, None)
                if task.media_id:
                    self.blobs._discard(task.media_id)
                    self.blobs._save()
                try:
                    self._path(task_id).unlink()
                except OSError as exc:
                    logger.warning("job record %s could not be removed: %s", task_id, exc)
                removed += 1
        return removed


_STORE: Optional[JobRegistry] = None


def shared_store() -> JobRegistry:
    """Process-wide store used by the HTTP handlers."""
    global _STORE
    if _STORE is None:
        _STORE = JobRegistry()
        _STORE.blobs.purge_expired()
        _STORE.purge_expired()
    return _STORE


def set_shared_store(store: Optional[JobRegistry]) -> None:
    global _STORE
    _STORE = store
