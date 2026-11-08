"""Muse chat browser driver adapted from czg86389-hub/muse2api's pinned engine.

Upstream DOM selectors and session flow: see UPSTREAM.md and UPSTREAM_LICENSE.
Each operation opens an isolated /thread/new; no caller shares a conversation.
"""

from __future__ import annotations

import asyncio
import logging
import time
from typing import AsyncIterator

try:
    from asyncio import timeout as async_timeout
except ImportError:
    from async_timeout import timeout as async_timeout

from .browser_session import BrowserSessions, DriverFailure
from .chat_js import CHAT_MODELS, IMAGE_MODELS, POLL_CHAT_JS, READY_JS, SEND_JS, SET_PROMPT_JS, VIDEO_MODELS
from .driver_base import DriverEvent, ImageDriverEvent, MuseDriver, VideoDriverEvent
from .image_flow import DEFAULT_IMAGE_TIMEOUT, run_image_flow
from .job_store import shared_store
from .media_protocol import (
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
from .video_flow import run_video_job

logger = logging.getLogger("muse_worker.engine")

__all__ = [
    "EngineMuseDriver", "READY_JS", "SET_PROMPT_JS", "SEND_JS", "POLL_CHAT_JS",
]

class EngineMuseDriver(BrowserSessions, MuseDriver):
    """Chat, image and video driver backed by an isolated browser thread per call."""

    def __init__(self, headless: bool = True, browser_executable_path: str | None = None,
                 blobs=None) -> None:
        BrowserSessions.__init__(self, headless=headless, browser_executable_path=browser_executable_path)
        self._blobs = blobs

    def blobs(self):
        """Media blob store shared with the job registry."""
        current = getattr(self, "_blobs", None)
        if current is None:
            current = shared_store().blobs
            self._blobs = current
        return current

    async def chat_completion(self, request: ChatRequest) -> AsyncIterator[DriverEvent]:
        if request.model not in CHAT_MODELS:
            yield ChatErrorEvent("Muse worker only supports chat models", "unsupported_model", 400)
            return
        async with self.account_lock(request.account):
            try:
                async with async_timeout(100):
                    async with self.session(request.account, wake=True) as (context, _):
                        page = await context.new_page()
                        await page.goto("https://muse.ai/thread/new", wait_until="domcontentloaded", timeout=20000)
                        if "login" in page.url.lower():
                            raise DriverFailure("Muse session redirected to login", "session_expired", 401)
                        await page.wait_for_function(READY_JS, timeout=20000)
                        baseline = await page.evaluate(POLL_CHAT_JS)
                        if baseline["count"]:
                            raise DriverFailure("Muse did not open an empty conversation", "thread_isolation_error")
                        if not await page.evaluate(SET_PROMPT_JS, request.prompt):
                            raise DriverFailure("Muse composer did not accept the complete prompt")
                        for _ in range(30):
                            if await page.evaluate(SEND_JS):
                                break
                            await asyncio.sleep(0.1)
                        else:
                            raise DriverFailure("Muse send button never became ready")
                        sent = ""
                        last_change = time.monotonic()
                        started = last_change
                        while True:
                            await asyncio.sleep(0.1)
                            state = await page.evaluate(POLL_CHAT_JS)
                            text = state["text"]
                            if not text:
                                if time.monotonic() - started > 40:
                                    raise DriverFailure("Muse first response token timed out", "upstream_timeout", 504)
                                continue
                            if not text.startswith(sent):
                                raise DriverFailure("Muse rewrote text already emitted", "stream_revision_error")
                            if text != sent:
                                delta, sent = text[len(sent):], text
                                last_change = time.monotonic()
                                yield ChatDeltaEvent(text_delta=delta)
                            # A visible Stop button always means generation is ongoing.
                            if not state["stop"] and time.monotonic() - last_change >= 0.5:
                                cookies = await context.cookies("https://muse.ai")
                                cookie_header = "; ".join(f"{c['name']}={c['value']}" for c in cookies)
                                yield ChatCompletedEvent(text=sent, cookies=cookie_header or None)
                                return
            except DriverFailure as exc:
                yield ChatErrorEvent(str(exc), exc.code, exc.status)
            except TimeoutError:
                yield ChatErrorEvent("Muse generation timed out", "upstream_timeout", 504)
            except asyncio.CancelledError:
                raise
            except Exception as exc:
                logger.warning("Muse browser operation failed (%s)", type(exc).__name__, exc_info=True)
                yield ChatErrorEvent("Muse browser operation failed", "browser_error", 502)

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
        if request.model not in IMAGE_MODELS:
            yield ChatErrorEvent("Muse image transport accepts muse-image only", "unsupported_model", 400)
            return
        async with self.account_lock(request.account):
            try:
                async with async_timeout(DEFAULT_IMAGE_TIMEOUT):
                    async with self.session(request.account, wake=True) as (context, _):
                        page = await context.new_page()
                        yield ImageProgressEvent(stage="submitted", detail=request.mode)
                        yield await run_image_flow(page, context, request, READY_JS, SET_PROMPT_JS, SEND_JS, self.blobs())
                        return
            except DriverFailure as exc:
                yield ChatErrorEvent(str(exc), exc.code, exc.status)
            except TimeoutError:
                yield ChatErrorEvent("Muse image generation timed out", "upstream_timeout", 504)
            except asyncio.CancelledError:
                raise
            except Exception as exc:
                logger.warning("Muse image operation failed (%s)", type(exc).__name__, exc_info=True)
                yield ChatErrorEvent("Muse browser operation failed", "browser_error", 502)

    async def video_generation(self, request: VideoRequest) -> AsyncIterator[VideoDriverEvent]:
        if request.model not in VIDEO_MODELS:
            yield ChatErrorEvent("Muse video transport accepts muse-video only", "unsupported_model", 400)
            return
        task = VideoTask(caller_id=request.caller_id, request_id=request.request_id,
                         model=request.model, prompt=request.prompt, duration=request.duration)
        async for update in run_video_job(self, request, task, self.blobs()):
            yield update

    async def probe_account(self, account: AccountInfo) -> ProbeResult:
        async with self.account_lock(account):
            try:
                async with async_timeout(30):
                    async with self.session(account) as (_, state):
                        return ProbeResult("ok", account.account_id, quota={
                            "session_status": state["status"], "vm_state": state.get("vm_state")})
            except DriverFailure as exc:
                status = "expired" if exc.status == 401 else "error"
                return ProbeResult(status, account.account_id, error=str(exc))
            except Exception as exc:
                logger.warning("Muse session probe failed (%s)", type(exc).__name__)
                return ProbeResult("error", account.account_id, error="Muse session probe failed")
