"""Mock driver for offline testing and CI verification without Chromium."""

from __future__ import annotations

import asyncio
import base64
import hashlib
from typing import AsyncIterator, Dict

from .driver_base import DriverEvent, ImageDriverEvent, MuseDriver, VideoDriverEvent
from .image_flow import media_public_url
from .mock_media import MOCK_VIDEO_BYTES, build_png_bytes
from .media_protocol import (
    ImageCompletedEvent,
    ImageItem,
    ImageProgressEvent,
    ImageRequest,
    VideoRequest,
    VideoTask,
)
from .protocol import (
    AccountInfo,
    ChatCompletedEvent,
    ChatDeltaEvent,
    ChatErrorEvent,
    ChatRequest,
    ProbeResult,
)


def _trigger_value(prompt: str, name: str) -> str | None:
    marker = f"[{name}:"
    if marker not in prompt:
        return None
    return prompt.split(marker, 1)[1].split("]", 1)[0]


def _trigger_kwarg(prompt: str, name: str) -> str | None:
    marker = f"[{name}="
    if marker not in prompt:
        return None
    return prompt.split(marker, 1)[1].split("]", 1)[0]


class MockMuseDriver(MuseDriver):
    """Offline mock driver providing deterministic test responses."""

    def __init__(self, chunk_delay: float = 0.005) -> None:
        self.chunk_delay = chunk_delay
        self._account_locks: Dict[str, asyncio.Lock] = {}
        self.active_requests: int = 0

    def _get_account_lock(self, account_id: str) -> asyncio.Lock:
        if account_id not in self._account_locks:
            self._account_locks[account_id] = asyncio.Lock()
        return self._account_locks[account_id]

    async def chat_completion(
        self, request: ChatRequest
    ) -> AsyncIterator[DriverEvent]:
        account_id = request.account.account_id or "default"
        lock = self._get_account_lock(account_id)

        # Enforce physical per-account locking
        async with lock:
            self.active_requests += 1
            try:
                # 1. Validation check
                if not request.account.cookies:
                    yield ChatErrorEvent(
                        error="missing muse session cookies",
                        code="auth_error",
                        status=401,
                    )
                    return

                if "invalid_cookie" in request.account.cookies:
                    yield ChatErrorEvent(
                        error="muse session cookies expired or invalid",
                        code="session_expired",
                        status=401,
                    )
                    return

                prompt = request.prompt

                # 2. Trigger check for deliberate error simulation
                if "[trigger:throttle]" in prompt:
                    yield ChatErrorEvent(
                        error="rate limit exceeded on muse account",
                        code="rate_limit_exceeded",
                        status=429,
                    )
                    return

                if "[trigger:error:" in prompt:
                    # Format: [trigger:error:status:code:message]
                    parts = prompt.split("[trigger:error:")[1].split("]")[0].split(":")
                    status = int(parts[0]) if len(parts) > 0 and parts[0].isdigit() else 500
                    code = parts[1] if len(parts) > 1 else "upstream_error"
                    msg = parts[2] if len(parts) > 2 else "triggered error"
                    yield ChatErrorEvent(error=msg, code=code, status=status)
                    return

                # 3. Simulate reasoning if triggered or requested
                reasoning_text = ""
                if "[trigger:reasoning:" in prompt:
                    reasoning_text = prompt.split("[trigger:reasoning:")[1].split("]")[0]
                    for r_chunk in reasoning_text.split(" "):
                        if self.chunk_delay > 0:
                            await asyncio.sleep(self.chunk_delay)
                        yield ChatDeltaEvent(reasoning_delta=r_chunk + " ")

                # 4. Simulate response text
                reply_text = f"Mock Muse response to: {prompt.strip()}"
                if "[reply:" in prompt:
                    reply_text = prompt.split("[reply:")[1].split("]")[0]

                # Stream out in words/chunks
                chunks = reply_text.split(" ")
                for i, chunk in enumerate(chunks):
                    if self.chunk_delay > 0:
                        await asyncio.sleep(self.chunk_delay)
                    suffix = " " if i < len(chunks) - 1 else ""
                    yield ChatDeltaEvent(text_delta=chunk + suffix)

                # 5. Check if updated cookies triggered
                new_cookies = None
                if "[trigger:cookie:" in prompt:
                    new_cookies = prompt.split("[trigger:cookie:")[1].split("]")[0]

                # 6. Complete event
                input_tokens = len(prompt) // 4 + 1
                output_tokens = len(reply_text) // 4 + 1
                reasoning_tokens = len(reasoning_text) // 4 if reasoning_text else 0
                yield ChatCompletedEvent(
                    finish_reason="stop",
                    text=reply_text,
                    reasoning=reasoning_text,
                    usage={
                        "prompt_tokens": input_tokens,
                        "completion_tokens": output_tokens,
                        "reasoning_tokens": reasoning_tokens,
                        "total_tokens": input_tokens + output_tokens + reasoning_tokens,
                    },
                    cookies=new_cookies,
                )
            finally:
                self.active_requests -= 1

    async def image_generation(self, request: ImageRequest) -> AsyncIterator[ImageDriverEvent]:
        async for event in self._run_image(request):
            yield event

    async def image_edit(self, request: ImageRequest) -> AsyncIterator[ImageDriverEvent]:
        if not request.references:
            yield ChatErrorEvent("an image edit needs at least one reference image", "missing_reference", 400)
            return
        async for event in self._run_image(request):
            yield event

    async def _run_image(self, request: ImageRequest) -> AsyncIterator[ImageDriverEvent]:
        account_id = request.account.account_id or "default"
        async with self._get_account_lock(account_id):
            self.active_requests += 1
            try:
                if not request.account.cookies:
                    yield ChatErrorEvent(error="missing muse session cookies", code="auth_error", status=401)
                    return
                if "invalid_cookie" in request.account.cookies:
                    yield ChatErrorEvent(error="muse session cookies expired or invalid", code="session_expired", status=401)
                    return
                if "[trigger:throttle]" in request.prompt:
                    yield ChatErrorEvent(error="rate limit exceeded on muse account", code="rate_limit_exceeded", status=429)
                    return
                if "[trigger:upload_fail]" in request.prompt:
                    yield ChatErrorEvent(error="reference image upload was not confirmed", code="upload_unconfirmed", status=502)
                    return
                if "[trigger:no_media]" in request.prompt:
                    yield ChatErrorEvent(error="Muse returned no usable image for this request", code="media_empty", status=502)
                    return

                yield ImageProgressEvent(stage="submitted", detail=request.mode)
                if self.chunk_delay > 0:
                    await asyncio.sleep(self.chunk_delay)

                count = request.n
                override = _trigger_kwarg(request.prompt, "image:n")
                if override and override.isdigit():
                    count = max(1, int(override))
                url_mode = request.response_format == "url"
                blobs = self.blobs() if url_mode else None
                items = []
                for index in range(count):
                    side = 8 if index == 0 else 4
                    raw = build_png_bytes(side, side)
                    entry = ImageItem(content_type="image/png", width=side, height=side)
                    if url_mode and blobs is not None:
                        entry.media_id = blobs.store(raw, "image/png")
                        entry.url = media_public_url(entry.media_id)
                        if not entry.url:
                            entry.b64_json = base64.b64encode(raw).decode("ascii")
                    else:
                        entry.b64_json = base64.b64encode(raw).decode("ascii")
                    items.append(entry)
                cookies = _trigger_value(request.prompt, "trigger:cookie")
                usage = {"images": len(items)}
                if "[trigger:ref_hash]" in request.prompt and request.references:
                    # Proves the reference bytes really reached the worker, not
                    # just the filename, without echoing the image itself.
                    usage["reference_sha256"] = hashlib.sha256(
                        request.references[0].decoded_bytes()).hexdigest()
                yield ImageCompletedEvent(
                    finish_reason="stop",
                    images=[item.to_dict() for item in items],
                    usage=usage,
                    cookies=cookies,
                    thread_url="https://muse.ai/thread/mock",
                )
            finally:
                self.active_requests -= 1

    async def video_generation(self, request: VideoRequest) -> AsyncIterator[VideoDriverEvent]:
        task = VideoTask(
            id=f"vid_mock_{request.request_id or 'job'}",
            caller_id=request.caller_id,
            request_id=request.request_id,
            model=request.model,
            prompt=request.prompt,
            duration=request.duration,
        )
        account_id = request.account.account_id or "default"
        async with self._get_account_lock(account_id):
            if not request.account.cookies:
                yield ChatErrorEvent(error="missing muse session cookies", code="auth_error", status=401)
                return
            if "[video:fail]" in request.prompt:
                task.status = "failed"
                task.stage = "failed"
                task.error, task.error_code = "mock video failure", "browser_error"
                yield task
                return
            blobs = self.blobs()
            if blobs is None:
                yield ChatErrorEvent(error="worker media store is not bound", code="internal_error", status=500)
                return
            task.status = "processing"
            task.stage = "waiting_media"
            task.progress = 50
            yield task
            if self.chunk_delay > 0:
                await asyncio.sleep(self.chunk_delay)
            task.media_id = blobs.store(MOCK_VIDEO_BYTES, "video/mp4")
            task.content_type = "video/mp4"
            task.size_bytes = len(MOCK_VIDEO_BYTES)
            task.status = "completed"
            task.stage = "completed"
            task.progress = 100
            yield task

    async def probe_account(self, account: AccountInfo) -> ProbeResult:
        if not account.cookies:
            return ProbeResult(
                status="error",
                account_id=account.account_id,
                error="empty cookies",
            )
        if "invalid_cookie" in account.cookies:
            return ProbeResult(
                status="expired",
                account_id=account.account_id,
                error="session expired",
            )
        return ProbeResult(
            status="ok",
            account_id=account.account_id,
            quota={"credits": 100, "status": "active"},
        )
