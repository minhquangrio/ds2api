"""Image and video RPC routes for the Muse worker.

Kept separate from ``server.py`` so the chat handler stays readable. All routes
share the worker secret; caller ownership for video jobs is carried by the
``caller_id`` Go passes through, because the worker (not the gateway) owns the
durable job record.
"""

from __future__ import annotations

import asyncio
import logging
import os
import time
from typing import Optional, Tuple

from aiohttp import web

from .chat_js import IMAGE_MODELS, VIDEO_MODELS
from .driver_base import MuseDriver
from .http_common import (
    DRIVER_KEY,
    SECRET_KEY,
    STORE_KEY,
    check_auth,
    error_response,
    read_json,
    unauthorized,
)
from .job_store import STATUS_COMPLETED, STATUS_FAILED, JobRegistry
from .media_protocol import (
    ImageCompletedEvent,
    ImageProgressEvent,
    ImageRequest,
    VideoRequest,
    VideoTask,
)
from .protocol import ChatErrorEvent
from .video_flow import video_content_extension

logger = logging.getLogger("muse_worker.media")

MAX_IMAGES = int(os.getenv("MUSE_IMAGE_MAX_N", "4"))
MAX_REFERENCE_BYTES = int(os.getenv("MUSE_REFERENCE_MAX_BYTES", str(16 << 20)))
RESPONSE_FORMATS = {"b64_json", "url"}
JOB_FIELDS = ("status", "stage", "progress", "media_id", "content_type", "size_bytes", "error", "error_code")


def _bad_request(message: str) -> web.Response:
    return error_response(message, "invalid_request", 400)


def validate_image_request(request: ImageRequest, edit: bool) -> Optional[str]:
    if request.model not in IMAGE_MODELS:
        return f"image transport accepts muse-image only, got {request.model!r}"
    if not request.prompt.strip():
        return "prompt must not be empty"
    if request.n < 1 or request.n > MAX_IMAGES:
        return f"n must be between 1 and {MAX_IMAGES}"
    if request.response_format not in RESPONSE_FORMATS:
        return "response_format must be b64_json or url"
    if edit and not request.references:
        return "an image edit needs at least one reference image"
    for item in request.references:
        if not item.data_base64:
            return "reference image payload is empty"
        if len(item.data_base64) * 3 // 4 > MAX_REFERENCE_BYTES:
            return "reference image exceeds the configured size limit"
    return None


def validate_video_request(request: VideoRequest) -> Optional[str]:
    if request.model not in VIDEO_MODELS:
        return f"video transport accepts muse-video only, got {request.model!r}"
    if not request.prompt.strip():
        return "prompt must not be empty"
    return None


def image_response(request: ImageRequest, completed: ImageCompletedEvent) -> web.Response:
    return web.json_response({
        "status": "ok",
        "request_id": request.request_id,
        "model": request.model,
        "created": int(time.time()),
        "data": completed.images,
        "usage": completed.usage or {},
        "cookies": completed.cookies,
        "thread_url": completed.thread_url,
    })


async def _run_image(request: web.Request, edit: bool) -> web.Response:
    if not check_auth(request, request.app[SECRET_KEY]):
        return unauthorized()
    payload, error = await read_json(request)
    if error is not None:
        return error
    try:
        image_request = ImageRequest.from_dict(payload)
    except Exception as exc:
        return _bad_request(f"invalid image request: {exc}")
    if edit:
        image_request.mode = "edit"
    invalid = validate_image_request(image_request, edit)
    if invalid:
        return _bad_request(invalid)

    driver: MuseDriver = request.app[DRIVER_KEY]
    stream = driver.image_edit(image_request) if edit else driver.image_generation(image_request)
    completed: Optional[ImageCompletedEvent] = None
    failure: Optional[ChatErrorEvent] = None
    try:
        async for event in stream:
            if isinstance(event, ImageProgressEvent):
                continue
            if isinstance(event, ImageCompletedEvent):
                completed = event
                break
            if isinstance(event, ChatErrorEvent):
                failure = event
                break
    except asyncio.CancelledError:
        raise
    except Exception as exc:
        logger.warning("Muse image operation crashed (%s)", type(exc).__name__, exc_info=True)
        return error_response("Muse image operation failed", "internal_error", 500)
    finally:
        await stream.aclose()

    if failure is not None:
        return web.json_response(failure.to_dict(), status=failure.status)
    if completed is None:
        return error_response("driver ended without completed", "incomplete_stream", 502)
    return image_response(image_request, completed)


async def handle_images_generations(request: web.Request) -> web.Response:
    return await _run_image(request, edit=False)


async def handle_images_edits(request: web.Request) -> web.Response:
    return await _run_image(request, edit=True)


def _merge_task(target: VideoTask, update: VideoTask) -> VideoTask:
    for field in JOB_FIELDS:
        setattr(target, field, getattr(update, field))
    return target


def _spawn_video_job(driver: MuseDriver, store: JobRegistry, request: VideoRequest, task: VideoTask) -> None:
    handle = asyncio.create_task(_consume_video_job(driver, store, request, task))
    store.register_handle(task.id, handle)
    handle.add_done_callback(lambda _done: store.forget_handle(task.id))


async def _consume_video_job(driver: MuseDriver, store: JobRegistry, request: VideoRequest, task: VideoTask) -> None:
    try:
        async for event in driver.video_generation(request):
            if isinstance(event, VideoTask):
                store.update(_merge_task(task, event))
            elif isinstance(event, ChatErrorEvent):
                task.status = STATUS_FAILED
                task.stage = "failed"
                task.error, task.error_code = event.error, event.code
                store.update(task)
            elif isinstance(event, ImageProgressEvent):
                task.stage = event.stage
                store.update(task)
    except asyncio.CancelledError:
        task.status = STATUS_FAILED
        task.stage = "cancelled"
        task.error, task.error_code = "job cancelled", "job_cancelled"
        store.update(task)
        raise
    except Exception as exc:
        logger.warning("Muse video job %s failed (%s)", task.id, type(exc).__name__, exc_info=True)
        task.status = STATUS_FAILED
        task.stage = "failed"
        task.error, task.error_code = "Muse video job failed", "browser_error"
        store.update(task)


async def handle_video_create(request: web.Request) -> web.Response:
    if not check_auth(request, request.app[SECRET_KEY]):
        return unauthorized()
    payload, error = await read_json(request)
    if error is not None:
        return error
    try:
        video_request = VideoRequest.from_dict(payload)
    except Exception as exc:
        return _bad_request(f"invalid video request: {exc}")
    invalid = validate_video_request(video_request)
    if invalid:
        return _bad_request(invalid)

    store: JobRegistry = request.app[STORE_KEY]
    driver: MuseDriver = request.app[DRIVER_KEY]
    task = store.create(VideoTask(
        caller_id=video_request.caller_id,
        request_id=video_request.request_id,
        model=video_request.model,
        prompt=video_request.prompt,
        duration=video_request.duration,
    ))
    _spawn_video_job(driver, store, video_request, task)
    return web.json_response({"status": "ok", "task": task.to_dict()})


def _find_task(request: web.Request) -> Tuple[Optional[VideoTask], Optional[web.Response]]:
    if not check_auth(request, request.app[SECRET_KEY]):
        return None, unauthorized()
    store: JobRegistry = request.app[STORE_KEY]
    task_id = (request.match_info.get("video_id") or "").strip()
    caller_id = (request.query.get("caller_id") or "").strip()
    if not task_id:
        return None, _bad_request("video id is required")
    task = store.get(task_id, caller_id)
    if task is None:
        return None, error_response("video job not found", "not_found", 404)
    return task, None


async def handle_video_status(request: web.Request) -> web.Response:
    task, error = _find_task(request)
    if error is not None:
        return error
    return web.json_response({"status": "ok", "task": task.to_dict()})


async def handle_video_content(request: web.Request) -> web.Response:
    task, error = _find_task(request)
    if error is not None:
        return error
    store: JobRegistry = request.app[STORE_KEY]
    if task.status != STATUS_COMPLETED or not task.media_id:
        return error_response(f"video job is not completed ({task.status})", "job_incomplete", 409)
    data = store.blobs.read(task.media_id)
    if data is None:
        return error_response("video media expired", "media_expired", 410)
    return web.Response(
        body=data,
        content_type=task.content_type or "video/mp4",
        headers={"Content-Disposition": f'attachment; filename="{task.id}.{video_content_extension(task.content_type)}"'},
    )


async def handle_media_bytes(request: web.Request) -> web.Response:
    """Serve stored image bytes; the gateway enforces caller ownership upstream."""
    if not check_auth(request, request.app[SECRET_KEY]):
        return unauthorized()
    store: JobRegistry = request.app[STORE_KEY]
    media_id = (request.match_info.get("media_id") or "").strip()
    if not media_id:
        return _bad_request("media id is required")
    data = store.blobs.read(media_id)
    if data is None:
        return error_response("media not found or expired", "media_expired", 410)
    return web.Response(body=data, content_type=store.blobs.content_type(media_id))


async def handle_video_list(request: web.Request) -> web.Response:
    if not check_auth(request, request.app[SECRET_KEY]):
        return unauthorized()
    store: JobRegistry = request.app[STORE_KEY]
    caller_id = (request.query.get("caller_id") or "").strip()
    tasks = store.list(caller_id, limit=int(request.query.get("limit") or "50"))
    return web.json_response({"status": "ok", "data": [task.to_dict() for task in tasks]})


def add_media_routes(app: web.Application) -> None:
    app.router.add_post("/v1/images/generations", handle_images_generations)
    app.router.add_post("/v1/images/edits", handle_images_edits)
    app.router.add_post("/v1/videos", handle_video_create)
    app.router.add_get("/v1/videos", handle_video_list)
    app.router.add_get("/v1/videos/{video_id}", handle_video_status)
    app.router.add_get("/v1/videos/{video_id}/content", handle_video_content)
    app.router.add_get("/v1/media/{media_id}", handle_media_bytes)
