"""Engine-side browser flow for asynchronous Muse video jobs.

A video job outlives any HTTP request that should wait for it, so the worker
runs it in the background and keeps the durable record in
``job_store.JobRegistry``. This module only drives the browser and reports
progress; the HTTP layer turns those reports into polling responses.

Selectors are DS2API-original and NOT verified against a live Muse.ai account
(see media_js.py). Failure paths surface as ``ChatErrorEvent`` so the job record
shows a real reason instead of a fake "completed" state.
"""

from __future__ import annotations

import asyncio
import logging
import os
import time
from typing import AsyncIterator, Optional

try:
    from asyncio import timeout as async_timeout
except ImportError:
    from async_timeout import timeout as async_timeout

from .browser_session import DriverFailure
from .chat_js import READY_JS, SEND_JS, SET_PROMPT_JS
from .image_flow import CHAT_READY_TIMEOUT, host_allowed, submit_prompt, upload_references
from .job_store import STATUS_COMPLETED, STATUS_FAILED, BlobStore
from .media_js import POLL_VIDEO_JS
from .media_protocol import VideoRequest, VideoTask

logger = logging.getLogger("muse_worker.video")

DEFAULT_VIDEO_TIMEOUT = float(os.getenv("MUSE_VIDEO_TIMEOUT", "900"))
DEFAULT_MAX_VIDEO_BYTES = int(os.getenv("MUSE_VIDEO_MAX_BYTES", str(64 << 20)))
POLL_INTERVAL = float(os.getenv("MUSE_VIDEO_POLL_SECONDS", "1.0"))
STABLE_WINDOW = float(os.getenv("MUSE_VIDEO_STABLE_SECONDS", "2.0"))
PROGRESS_BY_STAGE = {
    "opening_thread": 10,
    "uploading_reference": 20,
    "submitting_prompt": 30,
    "waiting_media": 50,
    "downloading_media": 85,
}


def video_content_extension(content_type: str) -> str:
    declared = (content_type or "").lower()
    if "webm" in declared:
        return "webm"
    if "quicktime" in declared or "mov" in declared:
        return "mov"
    return "mp4"


class VideoFlow:
    """Browser steps for one video generation, driven inside an open session."""

    def __init__(self, page, context, ready_js: str = READY_JS) -> None:
        self.page = page
        self.context = context
        self.ready_js = ready_js

    async def open_thread(self) -> None:
        await self.page.goto("https://muse.ai/thread/new", wait_until="domcontentloaded", timeout=20000)
        if "login" in (self.page.url or "").lower():
            raise DriverFailure("Muse session redirected to login", "session_expired", 401)
        await self.page.wait_for_function(self.ready_js, timeout=CHAT_READY_TIMEOUT)
        baseline = await self.page.evaluate(POLL_VIDEO_JS)
        if baseline.get("count"):
            raise DriverFailure("Muse did not open an empty conversation", "thread_isolation_error")

    async def wait_for_source(self, first_timeout: float, overall: float) -> str:
        """Return the first video source that appeared in this thread only."""
        started = time.monotonic()
        last_change = started
        seen: Optional[str] = None
        while True:
            await asyncio.sleep(POLL_INTERVAL)
            state = await self.page.evaluate(POLL_VIDEO_JS)
            for src in state.get("sources") or []:
                if src != seen:
                    seen = src
                    last_change = time.monotonic()
            if seen and not state.get("stop") and time.monotonic() - last_change >= STABLE_WINDOW:
                return seen
            if not seen and time.monotonic() - started > first_timeout:
                raise DriverFailure("Muse produced no video stream in time", "upstream_timeout", 504)
            if time.monotonic() - started > overall:
                raise DriverFailure("Muse video generation timed out", "upstream_timeout", 504)

    async def download(self, src: str) -> tuple[bytes, str]:
        if not host_allowed(src):
            raise DriverFailure(f"refusing to fetch media from unlisted host: {src}", "media_host_denied", 502)
        response = await self.context.request.get(src, timeout=60000, max_redirects=5)
        if response.status == 401:
            raise DriverFailure("Muse media requires a session that has expired", "session_expired", 401)
        if response.status != 200:
            raise DriverFailure(f"Muse media fetch returned HTTP {response.status}", "media_fetch_failed", 502)
        if not host_allowed(response.url):
            raise DriverFailure(f"Muse media redirected to an unlisted host: {response.url}", "media_host_denied", 502)
        data = await response.body()
        if not data:
            raise DriverFailure("Muse returned an empty media payload", "media_empty", 502)
        if len(data) > DEFAULT_MAX_VIDEO_BYTES:
            raise DriverFailure(f"Muse media exceeds the size limit ({len(data)})", "media_too_large", 502)
        declared = (response.headers.get("content-type") or "").split(";")[0].strip().lower()
        if declared and not declared.startswith("video/") and declared != "application/octet-stream":
            raise DriverFailure(f"Muse media is not a video payload ({declared})", "media_type_invalid", 502)
        return data, declared or "video/mp4"

    async def renewed_cookies(self) -> Optional[str]:
        cookies = await self.context.cookies("https://muse.ai")
        header = "; ".join(f"{c['name']}={c['value']}" for c in cookies)
        return header or None


def progress_for(stage: str) -> int:
    return PROGRESS_BY_STAGE.get(stage, 5)


async def run_video_job(driver, request: VideoRequest, task: VideoTask,
                        blobs: BlobStore) -> AsyncIterator[VideoTask]:
    """Drive one video job and yield updated task records as it advances."""
    try:
        async with driver.account_lock(request.account):
            async with async_timeout(DEFAULT_VIDEO_TIMEOUT + 60):
                async with driver.session(request.account, wake=True) as (context, _state):
                    flow = VideoFlow(await context.new_page(), context)
                    task.stage = "opening_thread"
                    task.progress = progress_for(task.stage)
                    yield task
                    await flow.open_thread()

                    if request.references:
                        task.stage = "uploading_reference"
                        task.progress = progress_for(task.stage)
                        yield task
                        await upload_references(flow.page, request.references)

                    task.stage = "submitting_prompt"
                    task.progress = progress_for(task.stage)
                    yield task
                    await submit_prompt(flow.page, SET_PROMPT_JS, SEND_JS, request.prompt)

                    task.stage = "waiting_media"
                    task.progress = progress_for(task.stage)
                    yield task
                    src = await flow.wait_for_source(DEFAULT_VIDEO_TIMEOUT / 3, DEFAULT_VIDEO_TIMEOUT)

                    task.stage = "downloading_media"
                    task.progress = progress_for(task.stage)
                    yield task
                    data, content_type = await flow.download(src)
                    task.media_id = blobs.store(data, content_type)
                    task.content_type = content_type
                    task.size_bytes = len(data)
                    task.status = STATUS_COMPLETED
                    task.stage = "completed"
                    task.progress = 100
                    task.updated_at = time.time()
                    yield task
                    return
    except DriverFailure as exc:
        task.status = STATUS_FAILED
        task.stage = "failed"
        task.error, task.error_code = str(exc), exc.code
        yield task
    except asyncio.CancelledError:
        task.status = STATUS_FAILED
        task.stage = "cancelled"
        task.error, task.error_code = "job cancelled before completion", "job_cancelled"
        yield task
        raise
    except TimeoutError:
        task.status = STATUS_FAILED
        task.stage = "failed"
        task.error, task.error_code = "Muse video generation timed out", "upstream_timeout"
        yield task
    except Exception as exc:
        logger.warning("Muse video job failed (%s)", type(exc).__name__, exc_info=True)
        task.status = STATUS_FAILED
        task.stage = "failed"
        task.error, task.error_code = "Muse video job failed", "browser_error"
        yield task
