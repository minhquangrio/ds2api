"""Engine-side browser flow for Muse image generation and reference-image edits.

Lives in its own module (instead of growing ``driver_engine.py``) so the chat
driver stays readable and the line gate keeps its budget. The flow is shared by
images and videos: open an isolated thread, optionally upload references with a
confirmed ack, submit the prompt, then read only media that appeared in this
thread.

Every step that could hand back the wrong artifact fails loudly: an unconfirmed
upload, a non-empty baseline thread, media outside the last agent bubble, a
redirect off the allowlisted hosts, or a payload whose bytes are not an image.
"""

from __future__ import annotations

import asyncio
import base64
import logging
import os
import time
from typing import Any, Dict, List, Optional
from urllib.parse import urlsplit

from .browser_session import DriverFailure
from .media_js import (
    FILE_INPUT_SELECTOR_JS,
    FETCH_MEDIA_B64_JS,
    POLL_IMAGE_JS,
    UPLOAD_PREVIEW_JS,
)
from .media_protocol import ImageCompletedEvent, ImageItem, ImageRequest

logger = logging.getLogger("muse_worker.image")

CHAT_READY_TIMEOUT = 20.0
DEFAULT_IMAGE_TIMEOUT = float(os.getenv("MUSE_IMAGE_TIMEOUT", "240"))
DEFAULT_FIRST_MEDIA_TIMEOUT = float(os.getenv("MUSE_IMAGE_FIRST_TIMEOUT", "90"))
DEFAULT_MAX_IMAGE_BYTES = int(os.getenv("MUSE_IMAGE_MAX_BYTES", str(8 << 20)))
POLL_INTERVAL = float(os.getenv("MUSE_MEDIA_POLL_SECONDS", "0.25"))
STABLE_WINDOW = float(os.getenv("MUSE_MEDIA_STABLE_SECONDS", "1.0"))
UPLOAD_ACK_TIMEOUT = float(os.getenv("MUSE_UPLOAD_ACK_TIMEOUT", "20"))
UPLOAD_ACK_POLL = float(os.getenv("MUSE_UPLOAD_ACK_POLL_SECONDS", "0.2"))

IMAGE_MAGIC_PREFIXES = (
    (b"\x89PNG", "image/png"),
    (b"\xff\xd8\xff", "image/jpeg"),
    (b"GIF87a", "image/gif"),
    (b"GIF89a", "image/gif"),
    (b"RIFF", "image/webp"),
    (b"BM", "image/bmp"),
)


def allowed_hosts() -> List[str]:
    raw = os.getenv("MUSE_MEDIA_ALLOWED_HOSTS", "").strip() or "muse.ai"
    return [h.strip().lstrip(".").lower() for h in raw.split(",") if h.strip()]


def host_allowed(url: str) -> bool:
    host = (urlsplit(url).hostname or "").lower()
    if not host:
        return False
    return any(host == allowed or host.endswith("." + allowed) for allowed in allowed_hosts())


def sniff_image_type(data: bytes) -> Optional[str]:
    if len(data) < 12:
        return None
    for prefix, mime in IMAGE_MAGIC_PREFIXES:
        if data.startswith(prefix):
            if mime == "image/webp" and data[8:12] != b"WEBP":
                continue
            return mime
    return None


async def submit_prompt(page, set_prompt_js: str, send_js: str, prompt: str) -> None:
    """Set the composer text verbatim and press send, mirroring the chat driver."""
    if not await page.evaluate(set_prompt_js, prompt):
        raise DriverFailure("Muse composer did not accept the complete prompt")
    for _ in range(30):
        if await page.evaluate(send_js):
            return
        await asyncio.sleep(0.1)
    raise DriverFailure("Muse send button never became ready")


async def upload_references(page, references) -> None:
    """Attach reference images and require visible proof the composer took them."""
    if not references:
        return
    selector = await page.evaluate(FILE_INPUT_SELECTOR_JS)
    if not selector:
        raise DriverFailure("Muse composer exposes no image upload control", "upload_unsupported", 502)
    before = await page.evaluate(UPLOAD_PREVIEW_JS)
    payload = []
    for item in references:
        data = item.decoded_bytes()
        if not data:
            raise DriverFailure("reference image payload is empty", "invalid_request", 400)
        payload.append({"name": item.filename or "reference.png", "mimeType": item.content_type or "image/png", "buffer": data})
    await page.set_input_files(selector, payload)
    deadline = time.monotonic() + UPLOAD_ACK_TIMEOUT
    while time.monotonic() < deadline:
        after = await page.evaluate(UPLOAD_PREVIEW_JS)
        if after.get("previews", 0) > before.get("previews", 0) or after.get("files", 0) >= len(payload):
            return
        await asyncio.sleep(UPLOAD_ACK_POLL)
    raise DriverFailure("reference image upload was not confirmed", "upload_unconfirmed", 502)


async def read_media_bytes(context, page, src: str, limit: int) -> tuple[bytes, str]:
    """Materialize media bytes while the session is still open, then validate them."""
    if src.startswith("blob:") or src.startswith("data:"):
        fetched = await page.evaluate(FETCH_MEDIA_B64_JS, src)
        if not isinstance(fetched, dict) or not fetched.get("ok"):
            raise DriverFailure("Muse media URL was not readable from the page", "media_fetch_failed", 502)
        data = base64.b64decode(fetched.get("base64") or "")
    else:
        if not host_allowed(src):
            raise DriverFailure(f"refusing to fetch media from unlisted host: {src}", "media_host_denied", 502)
        response = await context.request.get(src, timeout=30000, max_redirects=5)
        if response.status == 401:
            raise DriverFailure("Muse media requires a session that has expired", "session_expired", 401)
        if response.status != 200:
            raise DriverFailure(f"Muse media fetch returned HTTP {response.status}", "media_fetch_failed", 502)
        if not host_allowed(response.url):
            raise DriverFailure(f"Muse media redirected to an unlisted host: {response.url}", "media_host_denied", 502)
        data = await response.body()
    if not data:
        raise DriverFailure("Muse returned an empty media payload", "media_empty", 502)
    if len(data) > limit:
        raise DriverFailure(f"Muse media exceeds the size limit ({len(data)} > {limit})", "media_too_large", 502)
    return data, sniff_image_type(data) or "application/octet-stream"


def build_items(raw: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    return [dict(entry) for entry in raw]


def media_public_url(media_id: str) -> Optional[str]:
    """Public base for worker-served media, only when the operator configured one."""
    base = os.getenv("MUSE_MEDIA_BASE_URL", "").strip().rstrip("/")
    if not base:
        return None
    return f"{base}/v1/media/{media_id}"


class ImageFlow:
    """Runs one image generation/edit against an already-open page and context."""

    def __init__(self, page, context, ready_js: str, poll_js: str = POLL_IMAGE_JS) -> None:
        self.page = page
        self.context = context
        self.ready_js = ready_js
        self.poll_js = poll_js

    async def open_thread(self) -> Dict[str, Any]:
        await self.page.goto("https://muse.ai/thread/new", wait_until="domcontentloaded", timeout=20000)
        if "login" in (self.page.url or "").lower():
            raise DriverFailure("Muse session redirected to login", "session_expired", 401)
        await self.page.wait_for_function(self.ready_js, timeout=CHAT_READY_TIMEOUT)
        baseline = await self.page.evaluate(self.poll_js)
        if baseline.get("count"):
            raise DriverFailure("Muse did not open an empty conversation", "thread_isolation_error")
        return baseline

    async def collect(self, request: ImageRequest, set_prompt_js: str, send_js: str):
        """Poll until every requested image is new, complete and stable."""
        await self.upload_and_send(request, set_prompt_js, send_js)
        started = time.monotonic()
        seen: Dict[str, Dict[str, Any]] = {}
        last_change = started
        while True:
            await asyncio.sleep(POLL_INTERVAL)
            state = await self.page.evaluate(self.poll_js)
            for item in state.get("items") or []:
                src = item.get("src")
                if src and src not in seen and item.get("complete") and item.get("width", 0) > 0:
                    seen[src] = item
                    last_change = time.monotonic()
            if len(seen) < request.n:
                if time.monotonic() - started > DEFAULT_FIRST_MEDIA_TIMEOUT:
                    raise DriverFailure("Muse produced no new image in time", "upstream_timeout", 504)
                continue
            if state.get("stop") or time.monotonic() - last_change < STABLE_WINDOW:
                if time.monotonic() - started > DEFAULT_IMAGE_TIMEOUT:
                    raise DriverFailure("Muse media generation timed out", "upstream_timeout", 504)
                continue
            return seen

    async def upload_and_send(self, request: ImageRequest, set_prompt_js: str, send_js: str) -> None:
        if request.is_edit:
            await upload_references(self.page, request.references)
        await submit_prompt(self.page, set_prompt_js, send_js, request.prompt)

    async def extract(self, seen: Dict[str, Dict[str, Any]], request: ImageRequest,
                      blobs=None) -> List[ImageItem]:
        items: List[ImageItem] = []
        for src, meta in list(seen.items())[: request.n]:
            data, mime = await read_media_bytes(self.context, self.page, src, DEFAULT_MAX_IMAGE_BYTES)
            if not mime.startswith("image/"):
                raise DriverFailure("Muse media payload is not a decodable image", "media_type_invalid", 502)
            entry = ImageItem(
                b64_json=base64.b64encode(data).decode("ascii"),
                content_type=mime,
                width=int(meta.get("width") or 0),
                height=int(meta.get("height") or 0),
            )
            if request.response_format == "url" and blobs is not None:
                entry.media_id = blobs.store(data, mime)
                entry.url = media_public_url(entry.media_id)
                if entry.url:
                    # Keep exactly one representation on the wire, as OpenAI does.
                    entry.b64_json = None
            items.append(entry)
        if not items:
            raise DriverFailure("Muse returned no usable image for this request", "media_empty", 502)
        return items

    async def renewed_cookies(self) -> Optional[str]:
        cookies = await self.context.cookies("https://muse.ai")
        header = "; ".join(f"{c['name']}={c['value']}" for c in cookies)
        return header or None


async def run_image_flow(page, context, request: ImageRequest, ready_js: str, set_prompt_js: str,
                         send_js: str, blobs=None) -> ImageCompletedEvent:
    """Complete a single-shot image operation; the caller owns lock, timeout, session."""
    flow = ImageFlow(page, context, ready_js)
    await flow.open_thread()
    seen = await flow.collect(request, set_prompt_js, send_js)
    items = await flow.extract(seen, request, blobs)
    return ImageCompletedEvent(
        finish_reason="stop",
        images=build_items([item.to_dict() for item in items]),
        cookies=await flow.renewed_cookies(),
        thread_url=getattr(page, "url", None),
        usage={"images": len(items)},
    )
