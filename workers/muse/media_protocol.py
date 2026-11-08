"""Media protocol dataclasses for the Muse image and video transports.

Serialization conventions match ``protocol.py``: request records carry
from_dict/to_dict, event records carry a hand-written to_dict with a "type"
field plus a single-data-line to_sse (the Go scanner relies on one line).
Media deliberately has its own events instead of widening the chat unions, so
chat ``isinstance`` checks stay unchanged.
"""

from __future__ import annotations

import base64
import json
from dataclasses import asdict, dataclass, field
from typing import Any, Dict, List, Optional

from .protocol import AccountInfo


@dataclass
class MediaAttachment:
    """Binary media handed to the worker in memory; never a DeepSeek file id."""

    filename: str = "reference.png"
    content_type: str = "image/png"
    data_base64: str = ""

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "MediaAttachment":
        data = data or {}
        return cls(
            filename=str(data.get("filename", "")).strip() or "reference.png",
            content_type=str(data.get("content_type", "")).strip() or "image/png",
            data_base64=str(data.get("data_base64", "")).strip(),
        )

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)

    def decoded_bytes(self) -> bytes:
        return base64.b64decode(self.data_base64) if self.data_base64 else b""


def attachments_from(raw: Any) -> List[MediaAttachment]:
    """Normalize one attachment, a list of them, or nothing at all."""
    items = raw if isinstance(raw, list) else ([raw] if isinstance(raw, dict) else [])
    return [MediaAttachment.from_dict(item) for item in items]


@dataclass
class ImageRequest:
    """Text-to-image and reference-image edit request."""

    request_id: str
    account: AccountInfo
    model: str = "muse-image"
    prompt: str = ""
    mode: str = "generation"
    n: int = 1
    size: Optional[str] = None
    response_format: str = "b64_json"
    caller_id: str = ""
    references: List[MediaAttachment] = field(default_factory=list)
    options: Optional[Dict[str, Any]] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "ImageRequest":
        raw_account = data.get("account") or {}
        try:
            count = int(data.get("n", 1))
        except (TypeError, ValueError):
            count = 1
        return cls(
            request_id=str(data.get("request_id", "")).strip(),
            account=AccountInfo.from_dict(raw_account),
            model=str(data.get("model", "muse-image")).strip(),
            prompt=str(data.get("prompt", "")),
            mode=str(data.get("mode", "generation")).strip().lower() or "generation",
            n=count,
            size=(str(data.get("size")).strip() if data.get("size") else None),
            response_format=str(data.get("response_format", "b64_json")).strip().lower() or "b64_json",
            caller_id=str(data.get("caller_id", "")).strip(),
            references=attachments_from(data.get("references") or data.get("reference_image")),
            options=data.get("options") or {},
        )

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)

    @property
    def is_edit(self) -> bool:
        return self.mode == "edit"


@dataclass
class VideoRequest:
    """Asynchronous video generation request."""

    request_id: str
    account: AccountInfo
    model: str = "muse-video"
    prompt: str = ""
    caller_id: str = ""
    duration: Optional[int] = None
    size: Optional[str] = None
    references: List[MediaAttachment] = field(default_factory=list)
    options: Optional[Dict[str, Any]] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "VideoRequest":
        raw_account = data.get("account") or {}
        try:
            duration = int(data["duration"]) if data.get("duration") is not None else None
        except (TypeError, ValueError):
            duration = None
        return cls(
            request_id=str(data.get("request_id", "")).strip(),
            account=AccountInfo.from_dict(raw_account),
            model=str(data.get("model", "muse-video")).strip(),
            prompt=str(data.get("prompt", "")),
            caller_id=str(data.get("caller_id", "")).strip(),
            duration=duration,
            size=(str(data.get("size")).strip() if data.get("size") else None),
            references=attachments_from(data.get("references")),
            options=data.get("options") or {},
        )

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)


@dataclass
class ImageProgressEvent:
    """Best-effort lifecycle signal for a media job; never carries media bytes."""

    stage: str = "queued"
    detail: str = ""

    def to_dict(self) -> Dict[str, Any]:
        return {"type": "progress", "stage": self.stage, "detail": self.detail}

    def to_sse(self) -> str:
        return f"event: progress\ndata: {json.dumps(self.to_dict(), ensure_ascii=False)}\n\n"


@dataclass
class ImageItem:
    """One generated image.

    b64_json is authoritative; url is only present when the operator configured
    MUSE_MEDIA_BASE_URL, so an internal file name is never handed to a client.
    """

    b64_json: Optional[str] = None
    url: Optional[str] = None
    media_id: Optional[str] = None
    content_type: str = "image/png"
    width: int = 0
    height: int = 0

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)


@dataclass
class ImageCompletedEvent:
    finish_reason: str = "stop"
    images: List[Dict[str, Any]] = field(default_factory=list)
    usage: Optional[Dict[str, Any]] = None
    cookies: Optional[str] = None
    thread_url: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        return {
            "type": "completed",
            "finish_reason": self.finish_reason,
            "images": self.images,
            "usage": self.usage or {},
            "cookies": self.cookies,
            "thread_url": self.thread_url,
        }

    def to_sse(self) -> str:
        return f"event: completed\ndata: {json.dumps(self.to_dict(), ensure_ascii=False)}\n\n"


@dataclass
class VideoTask:
    """Durable video job record. The worker owns it; Go only proxies and checks auth."""

    id: str = ""
    status: str = "queued"
    caller_id: str = ""
    request_id: str = ""
    model: str = "muse-video"
    prompt: str = ""
    created_at: float = 0.0
    updated_at: float = 0.0
    progress: int = 0
    stage: str = "queued"
    error: Optional[str] = None
    error_code: Optional[str] = None
    media_id: Optional[str] = None
    content_type: str = "video/mp4"
    size_bytes: int = 0
    duration: Optional[int] = None

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)

    def to_sse(self) -> str:
        return f"event: task\ndata: {json.dumps(self.to_dict(), ensure_ascii=False)}\n\n"

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> "VideoTask":
        data = data or {}
        return cls(
            id=str(data.get("id", "")).strip(),
            status=str(data.get("status", "queued")).strip() or "queued",
            caller_id=str(data.get("caller_id", "")).strip(),
            request_id=str(data.get("request_id", "")).strip(),
            model=str(data.get("model", "muse-video")).strip(),
            prompt=str(data.get("prompt", "")),
            created_at=float(data.get("created_at") or 0.0),
            updated_at=float(data.get("updated_at") or 0.0),
            progress=int(data.get("progress") or 0),
            stage=str(data.get("stage", "queued")).strip() or "queued",
            error=data.get("error"),
            error_code=data.get("error_code"),
            media_id=data.get("media_id"),
            content_type=str(data.get("content_type", "video/mp4")).strip() or "video/mp4",
            size_bytes=int(data.get("size_bytes") or 0),
            duration=data.get("duration") if isinstance(data.get("duration"), int) else None,
        )
