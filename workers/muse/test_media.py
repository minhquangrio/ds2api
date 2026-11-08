"""Offline tests for the Muse media contract: fixtures, stores, RPC routes, DOM flow.

Everything here runs without Chromium, without a Muse account and without
network access. The engine tests drive the same code paths the browser driver
uses through fake page/context objects that dispatch on JS-constant identity,
the same way ``test_engine.py`` does for chat.
"""

from __future__ import annotations

import asyncio
import base64
import dataclasses
import json
import os
import struct
import tempfile
import time
import unittest.mock
from pathlib import Path

import pytest
from aiohttp import web
from aiohttp.test_utils import AioHTTPTestCase

from . import image_flow, video_flow
from .chat_js import POLL_CHAT_JS, SEND_JS, SET_PROMPT_JS
from .driver_engine import EngineMuseDriver
from .driver_mock import MockMuseDriver
from .image_flow import DriverFailure
from .job_store import BlobStore, JobRegistry, MediaOverflow
from .media_js import (
    FETCH_MEDIA_B64_JS,
    FILE_INPUT_SELECTOR_JS,
    POLL_IMAGE_JS,
    POLL_VIDEO_JS,
    UPLOAD_PREVIEW_JS,
)
from .mock_media import MOCK_VIDEO_BYTES, build_png_bytes
from .media_protocol import (
    ImageCompletedEvent,
    ImageProgressEvent,
    ImageRequest,
    MediaAttachment,
    VideoRequest,
    VideoTask,
)
from .protocol import AccountInfo, ChatErrorEvent
from .server import create_app
from .server_media import validate_image_request

PNG = build_png_bytes()
PNG_B64 = base64.b64encode(PNG).decode("ascii")
ACCOUNT = {"account_id": "acc", "cookies": "hatch_sess=ok"}


def png_dimensions(data: bytes) -> tuple[int, int]:
    assert data[:8] == b"\x89PNG\r\n\x1a\n"
    width, height = struct.unpack(">II", data[16:24])
    return width, height


# --------------------------------------------------------------------------
# Fixtures and protocol shapes


def test_mock_png_fixture_is_deterministic_and_decodable() -> None:
    assert build_png_bytes() == PNG
    assert png_dimensions(PNG) == (8, 8)
    assert png_dimensions(build_png_bytes(4, 4)) == (4, 4)


def test_mock_video_fixture_is_a_transport_blob_only() -> None:
    # Documented as an opaque payload: the mock proves transport, not a codec.
    assert MOCK_VIDEO_BYTES.startswith(b"DS2API-MUSE-MOCK-VIDEO")


def test_image_request_round_trip_keeps_references() -> None:
    payload = {
        "request_id": "req_1",
        "account": ACCOUNT,
        "model": "muse-image",
        "prompt": "make it night",
        "mode": "edit",
        "n": 2,
        "size": "1024x1024",
        "response_format": "b64_json",
        "caller_id": "caller:abc",
        "references": [{"filename": "a.png", "content_type": "image/png", "data_base64": PNG_B64}],
    }
    request = ImageRequest.from_dict(payload)
    assert request.is_edit and request.n == 2 and request.caller_id == "caller:abc"
    assert request.references[0].decoded_bytes() == PNG
    assert ImageRequest.from_dict({**payload, "mode": "generation"}).mode == "generation"


def test_image_events_serialise_as_single_line_sse() -> None:
    completed = ImageCompletedEvent(images=[{"b64_json": PNG_B64, "content_type": "image/png"}], cookies="hatch_sess=x")
    raw = completed.to_sse()
    assert raw.startswith("event: completed\ndata: ") and raw.endswith("\n\n")
    assert "\n" not in raw.split("data: ", 1)[1].rsplit("\n\n", 1)[0]
    decoded = json.loads(raw.split("data: ", 1)[1].rsplit("\n\n", 1)[0])
    assert decoded["images"][0]["b64_json"] == PNG_B64
    assert ImageProgressEvent(stage="submitted").to_dict()["type"] == "progress"


def test_video_task_round_trip_survives_json() -> None:
    task = VideoTask(id="vid_1", status="completed", caller_id="caller:abc", media_id="med_1",
                     progress=100, size_bytes=len(MOCK_VIDEO_BYTES), duration=5)
    restored = VideoTask.from_dict(json.loads(json.dumps(task.to_dict())))
    assert restored == task


# --------------------------------------------------------------------------
# Blob and job stores


def test_blob_store_reads_back_and_enforces_size_limit() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        blobs = BlobStore(Path(tmp))
        media_id = blobs.store(PNG, "image/png")
        assert blobs.read(media_id) == PNG
        assert blobs.content_type(media_id) == "image/png"
        assert blobs.read("med_missing") is None
        with unittest.mock.patch.dict(os.environ, {"MUSE_MEDIA_MAX_BYTES": "1024"}):
            with pytest.raises(MediaOverflow):
                blobs.store(b"way too big for this test" * 200, "image/png")


def test_blob_store_purges_expired_entries() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        blobs = BlobStore(Path(tmp))
        media_id = blobs.store(PNG, "image/png")
        blobs._index[media_id]["created_at"] = time.time() - 10 ** 6
        assert blobs.purge_expired() == 1
        assert blobs.read(media_id) is None


def test_job_registry_is_durable_and_owner_scoped() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = JobRegistry(root, BlobStore(root))
        mine = store.create(VideoTask(caller_id="caller:me", prompt="p"))
        store.update(_with(mine, status="processing", stage="waiting_media", progress=50))
        assert store.get(mine.id, "caller:me") is not None
        assert store.get(mine.id, "caller:other") is None
        assert [t.id for t in store.list("caller:me")] == [mine.id]
        assert store.list("caller:other") == []

        restarted = JobRegistry(root, BlobStore(root))
        interrupted = restarted.get(mine.id, "caller:me")
        assert interrupted is not None and interrupted.status == "interrupted"
        assert interrupted.error_code == "job_interrupted"


def test_completed_jobs_survive_restart_and_keep_media() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = JobRegistry(root, BlobStore(root))
        task = store.create(VideoTask(caller_id="caller:me"))
        task.media_id = store.blobs.store(MOCK_VIDEO_BYTES, "video/mp4")
        store.update(_with(task, status="completed", progress=100))
        revived = JobRegistry(root, BlobStore(root)).get(task.id, "caller:me")
        assert revived is not None and revived.status == "completed"
        assert revived.media_id and revived.content_type == "video/mp4"


def _with(task: VideoTask, **fields) -> VideoTask:
    for key, value in fields.items():
        setattr(task, key, value)
    return task


# --------------------------------------------------------------------------
# Image RPC validation


def test_validate_image_request_rejects_bad_parameters() -> None:
    base = ImageRequest(request_id="r", account=AccountInfo("acc", "hatch_sess=ok"), prompt="cat")
    assert validate_image_request(base, edit=False) is None
    assert "muse-image" in (validate_image_request(_with_req(base, model="muse-spark"), False) or "")
    assert "empty" in (validate_image_request(_with_req(base, prompt="   "), False) or "")
    assert "n must be" in (validate_image_request(_with_req(base, n=9), False) or "")
    assert "response_format" in (validate_image_request(_with_req(base, response_format="svg"), False) or "")
    assert "reference" in (validate_image_request(base, edit=True) or "")


def _with_req(request: ImageRequest, **fields) -> ImageRequest:
    return dataclasses.replace(request, **fields)


class MediaServerTestCase(AioHTTPTestCase):
    """Exercises the image and video RPC surface against the mock driver."""

    async def get_application(self) -> web.Application:
        self.tmp = tempfile.mkdtemp()
        root = Path(self.tmp)
        self.store = JobRegistry(root, BlobStore(root))
        self.mock_driver = MockMuseDriver(chunk_delay=0.001)
        return create_app(driver=self.mock_driver, secret="test-secret-123", store=self.store)

    async def test_rejects_missing_secret(self) -> None:
        resp = await self.client.request("POST", "/v1/images/generations", json={"prompt": "cat"})
        assert resp.status == 401

    async def test_rejects_invalid_parameters_before_driver(self) -> None:
        headers = {"X-Worker-Secret": "test-secret-123"}
        resp = await self.client.request(
            "POST", "/v1/images/generations",
            json={"request_id": "r", "account": ACCOUNT, "model": "muse-image", "prompt": "cat", "n": 12},
            headers=headers)
        assert resp.status == 400
        assert "n must be" in (await resp.json())["error"]

        resp = await self.client.request(
            "POST", "/v1/images/edits",
            json={"request_id": "r", "account": ACCOUNT, "model": "muse-image", "prompt": "cat"},
            headers=headers)
        assert resp.status == 400 and "reference" in (await resp.json())["error"]

    async def test_generation_returns_b64_image(self) -> None:
        resp = await self.client.request(
            "POST", "/v1/images/generations",
            json={"request_id": "gen", "account": ACCOUNT, "model": "muse-image", "prompt": "a red fox"},
            headers={"X-Worker-Secret": "test-secret-123"})
        assert resp.status == 200
        body = await resp.json()
        assert body["status"] == "ok" and body["model"] == "muse-image"
        assert len(body["data"]) == 1
        assert base64.b64decode(body["data"][0]["b64_json"]) == PNG
        assert body["data"][0]["url"] is None
        assert body["usage"]["images"] == 1

    async def test_edit_proves_reference_bytes_reached_worker(self) -> None:
        payload = {
            "request_id": "edit", "account": ACCOUNT, "model": "muse-image",
            "prompt": "[trigger:ref_hash] brighter sky", "mode": "generation",
            "references": [{"filename": "in.png", "content_type": "image/png", "data_base64": PNG_B64}],
        }
        resp = await self.client.request("POST", "/v1/images/edits", json=payload,
                                          headers={"X-Worker-Secret": "test-secret-123"})
        assert resp.status == 200
        body = await resp.json()
        import hashlib

        assert body["usage"]["reference_sha256"] == hashlib.sha256(PNG).hexdigest()
        # The route, not the caller, decides the mode.
        assert body["data"][0]["content_type"] == "image/png"

    async def test_multiple_images_and_cookie_refresh(self) -> None:
        resp = await self.client.request(
            "POST", "/v1/images/generations",
            json={"request_id": "many", "account": ACCOUNT, "model": "muse-image",
                  "prompt": "[image:n=3] [trigger:cookie:hatch_sess=renewed] stars"},
            headers={"X-Worker-Secret": "test-secret-123"})
        body = await resp.json()
        assert len(body["data"]) == 3
        assert body["cookies"] == "hatch_sess=renewed"

    async def test_upstream_errors_keep_status_and_code(self) -> None:
        headers = {"X-Worker-Secret": "test-secret-123"}
        for prompt, status, code in (
            ("[trigger:throttle] x", 429, "rate_limit_exceeded"),
            ("[trigger:no_media] x", 502, "media_empty"),
            ("[trigger:upload_fail] x", 502, "upload_unconfirmed"),
        ):
            resp = await self.client.request(
                "POST", "/v1/images/generations",
                json={"request_id": "err", "account": ACCOUNT, "model": "muse-image", "prompt": prompt},
                headers=headers)
            assert resp.status == status, prompt
            assert (await resp.json())["code"] == code, prompt

        resp = await self.client.request(
            "POST", "/v1/images/generations",
            json={"request_id": "err", "account": {"account_id": "a", "cookies": ""},
                  "model": "muse-image", "prompt": "x"}, headers=headers)
        assert resp.status == 401

    async def test_url_mode_stores_media_and_serves_bytes(self) -> None:
        headers = {"X-Worker-Secret": "test-secret-123"}
        with unittest.mock.patch.dict(os.environ, {"MUSE_MEDIA_BASE_URL": "https://media.example.test"}):
            resp = await self.client.request(
                "POST", "/v1/images/generations",
                json={"request_id": "url", "account": ACCOUNT, "model": "muse-image",
                      "prompt": "waves", "response_format": "url"}, headers=headers)
            body = await resp.json()
            item = body["data"][0]
            assert item["b64_json"] is None and item["media_id"]
            assert item["url"] == f"https://media.example.test/v1/media/{item['media_id']}"
            fetched = await self.client.request("GET", f"/v1/media/{item['media_id']}", headers=headers)
            assert fetched.status == 200
            assert await fetched.read() == PNG

        missing = await self.client.request("GET", "/v1/media/med_nope", headers=headers)
        assert missing.status == 410

    async def test_video_job_create_poll_and_content(self) -> None:
        headers = {"X-Worker-Secret": "test-secret-123"}
        resp = await self.client.request(
            "POST", "/v1/videos",
            json={"request_id": "v1", "account": ACCOUNT, "model": "muse-video",
                  "prompt": "a slow orbit", "caller_id": "caller:me", "duration": 5}, headers=headers)
        assert resp.status == 200
        task = (await resp.json())["task"]
        assert task["status"] in ("queued", "processing") and task["caller_id"] == "caller:me"

        completed = None
        for _ in range(50):
            status = await self.client.request("GET", f"/v1/videos/{task['id']}?caller_id=caller:me", headers=headers)
            assert status.status == 200
            completed = (await status.json())["task"]
            if completed["status"] == "completed":
                break
            await asyncio.sleep(0.02)
        assert completed and completed["status"] == "completed", completed
        assert completed["progress"] == 100 and completed["size_bytes"] == len(MOCK_VIDEO_BYTES)

        content = await self.client.request("GET", f"/v1/videos/{completed['id']}/content", headers=headers)
        assert content.status == 200
        assert await content.read() == MOCK_VIDEO_BYTES
        assert "video/mp4" in content.headers["Content-Type"]

        other = await self.client.request("GET", f"/v1/videos/{completed['id']}?caller_id=caller:other", headers=headers)
        assert other.status == 404

        listed = await self.client.request("GET", "/v1/videos?caller_id=caller:me", headers=headers)
        assert [t["id"] for t in (await listed.json())["data"]] == [completed["id"]]

    async def test_video_failure_records_reason(self) -> None:
        headers = {"X-Worker-Secret": "test-secret-123"}
        resp = await self.client.request(
            "POST", "/v1/videos",
            json={"request_id": "v2", "account": ACCOUNT, "model": "muse-video",
                  "prompt": "[video:fail] broken", "caller_id": "caller:me"}, headers=headers)
        task = (await resp.json())["task"]
        failed = None
        for _ in range(50):
            status = await self.client.request("GET", f"/v1/videos/{task['id']}?caller_id=caller:me", headers=headers)
            failed = (await status.json())["task"]
            if failed["status"] == "failed":
                break
            await asyncio.sleep(0.02)
        assert failed and failed["status"] == "failed" and failed["error_code"] == "browser_error"
        incomplete = await self.client.request("GET", f"/v1/videos/{task['id']}/content", headers=headers)
        assert incomplete.status == 409


# --------------------------------------------------------------------------
# Engine DOM flow through scripted fakes


class MediaResponse:
    def __init__(self, url: str, body: bytes, status: int = 200, content_type: str = "image/png") -> None:
        self.url = url
        self._body = body
        self.status = status
        self.headers = {"content-type": content_type}

    async def read(self) -> bytes:
        return self._body

    async def body(self) -> bytes:
        return self._body


class FakeMediaPage:
    """Dispatches on JS constant identity, mirroring test_engine.py's contract."""

    url = "https://muse.ai/thread/new"

    def __init__(self, baseline_count: int = 0, uploads_confirm: bool = True,
                 items=None, media_status: int = 200) -> None:
        self.baseline_count = baseline_count
        self.uploads_confirm = uploads_confirm
        self.scripted_items = items if items is not None else [
            {"src": "blob:https://muse.ai/render-1", "complete": True, "width": 1024, "height": 1024, "alt": ""}
        ]
        self.media_status = media_status
        self.polls = 0
        self.prompt = None
        self.sent = False
        self.uploaded = None

    async def goto(self, url, **kwargs) -> None:
        self.url = url

    async def wait_for_function(self, script, **kwargs) -> None:
        pass

    async def set_input_files(self, selector, files) -> None:
        self.uploaded = {"selector": selector, "files": files}

    async def evaluate(self, script, *args):
        if script == FILE_INPUT_SELECTOR_JS:
            return "#ref-upload"
        if script == UPLOAD_PREVIEW_JS:
            # An app that never accepted the file reports no file and no preview.
            attached = 1 if (self.uploaded and self.uploads_confirm) else 0
            return {"previews": attached, "files": attached}
        if script == SET_PROMPT_JS:
            self.prompt = args[0]
            return self.prompt == args[0]
        if script == SEND_JS:
            self.sent = True
            return True
        if script == FETCH_MEDIA_B64_JS:
            if self.media_status != 200:
                return {"ok": False, "status": self.media_status}
            return {"ok": True, "status": 200, "contentType": "image/png",
                    "base64": PNG_B64, "size": len(PNG)}
        if script == POLL_IMAGE_JS:
            if not self.sent:
                return {"count": self.baseline_count, "stop": False, "text": "", "items": []}
            self.polls += 1
            return {"count": 1, "stop": self.polls < 2, "text": "", "items": list(self.scripted_items)}
        if script == POLL_VIDEO_JS:
            if not self.sent:
                return {"count": self.baseline_count, "stop": False, "sources": [], "text": ""}
            self.polls += 1
            sources = ["https://muse.ai/media/out.mp4"] if self.polls >= 2 else []
            return {"count": 1, "stop": self.polls < 2, "sources": sources, "text": ""}
        assert script == POLL_CHAT_JS
        return {"count": 0, "text": "", "stop": False}


class FakeMediaContext:
    def __init__(self, page: FakeMediaPage, media_url: str = "https://muse.ai/media/render-1.png") -> None:
        self.page = page
        self.media_url = media_url
        self.request = self
        self.closed = False
        self.fetched: list[str] = []

    async def add_cookies(self, cookies) -> None:
        self.injected = cookies

    async def new_page(self) -> FakeMediaPage:
        return self.page

    async def cookies(self, url) -> list[dict]:
        return [{"name": "hatch_sess", "value": "renewed"}]

    async def close(self) -> None:
        self.closed = True

    async def get(self, url, **kwargs):
        if url == "https://muse.ai/api/session":
            return _SessionResponse()
        self.fetched.append(url)
        if url.endswith(".mp4"):
            return MediaResponse(url, MOCK_VIDEO_BYTES, content_type="video/mp4")
        if url == self.media_url:
            return MediaResponse(url, PNG, content_type="image/png")
        if url == "https://elsewhere.test/render.png":
            return MediaResponse("https://muse.ai.cdn-evil.test/render.png", PNG)
        if url == "https://muse.ai/media/huge.bin":
            return MediaResponse(url, b"0" * 4096, content_type="image/png")
        return MediaResponse(url, b"", status=404)


class _SessionResponse:
    status = 200

    async def json(self) -> dict:
        return {"status": "assigned", "vm_id": "fixture", "vm_state": "RUNNING"}


def _media_driver(page: FakeMediaPage, media_url: str = "https://muse.ai/media/render-1.png") -> tuple[EngineMuseDriver, FakeMediaContext]:
    context = FakeMediaContext(page, media_url)

    class FakeBrowser:
        def __init__(self, ctx) -> None:
            self.context = ctx
            self.options: dict = {}

        async def new_context(self, **kwargs):
            self.options = kwargs
            return self.context

    driver = EngineMuseDriver()
    driver._browser = FakeBrowser(context)
    return driver, context


def _fast_media(monkeypatch, tmp_path: Path) -> BlobStore:
    monkeypatch.setattr(image_flow, "POLL_INTERVAL", 0.01)
    monkeypatch.setattr(image_flow, "STABLE_WINDOW", 0.01)
    monkeypatch.setattr(image_flow, "UPLOAD_ACK_TIMEOUT", 0.05)
    monkeypatch.setattr(image_flow, "UPLOAD_ACK_POLL", 0.01)
    monkeypatch.setattr(video_flow, "POLL_INTERVAL", 0.01)
    monkeypatch.setattr(video_flow, "STABLE_WINDOW", 0.01)
    return BlobStore(tmp_path)


@pytest.mark.asyncio
async def test_engine_image_generation_reads_new_agent_media(monkeypatch, tmp_path) -> None:
    blobs = _fast_media(monkeypatch, tmp_path)
    page = FakeMediaPage()
    driver, context = _media_driver(page)
    driver.bind_blobs(blobs)
    request = ImageRequest(request_id="gen", account=AccountInfo("acc", "hatch_sess=ok"),
                           model="muse-image", prompt="a red fox")

    events = [event async for event in driver.image_generation(request)]

    assert isinstance(events[0], ImageProgressEvent) and events[0].stage == "submitted"
    completed = events[-1]
    assert isinstance(completed, ImageCompletedEvent), completed
    assert completed.images[0]["width"] == 1024
    assert base64.b64decode(completed.images[0]["b64_json"]) == PNG
    assert completed.cookies == "hatch_sess=renewed"
    assert page.prompt == "a red fox" and page.sent
    assert context.closed
    assert not driver.account_lock(AccountInfo("acc", "")).locked()


@pytest.mark.asyncio
async def test_engine_edit_uploads_reference_then_generates(monkeypatch, tmp_path) -> None:
    blobs = _fast_media(monkeypatch, tmp_path)
    page = FakeMediaPage()
    driver, context = _media_driver(page)
    driver.bind_blobs(blobs)
    request = ImageRequest(request_id="edit", account=AccountInfo("acc", "hatch_sess=ok"),
                           model="muse-image", prompt="brighter sky", mode="edit",
                           references=[MediaAttachment("in.png", "image/png", PNG_B64)])

    events = [event async for event in driver.image_edit(request)]

    assert isinstance(events[-1], ImageCompletedEvent)
    attached = page.uploaded["files"][0]
    assert attached["name"] == "in.png" and attached["mimeType"] == "image/png"
    assert attached["buffer"] == PNG
    assert page.uploaded["selector"] == "#ref-upload"
    assert context.closed


@pytest.mark.asyncio
async def test_engine_edit_without_reference_fails_before_browser(monkeypatch, tmp_path) -> None:
    blobs = _fast_media(monkeypatch, tmp_path)
    page = FakeMediaPage()
    driver, _context = _media_driver(page)
    driver.bind_blobs(blobs)
    request = ImageRequest(request_id="edit", account=AccountInfo("acc", "hatch_sess=ok"),
                           model="muse-image", prompt="brighter sky", mode="edit")

    events = [event async for event in driver.image_edit(request)]

    assert isinstance(events[0], ChatErrorEvent) and events[0].code == "missing_reference"
    assert page.prompt is None and not page.sent


@pytest.mark.asyncio
async def test_engine_upload_without_ack_never_submits(monkeypatch, tmp_path) -> None:
    blobs = _fast_media(monkeypatch, tmp_path)
    monkeypatch.setattr(image_flow, "DEFAULT_IMAGE_TIMEOUT", 5.0)
    page = FakeMediaPage(uploads_confirm=False)
    driver, context = _media_driver(page)
    driver.bind_blobs(blobs)
    request = ImageRequest(request_id="edit", account=AccountInfo("acc", "hatch_sess=ok"),
                           model="muse-image", prompt="brighter sky", mode="edit",
                           references=[MediaAttachment("in.png", "image/png", PNG_B64)])

    events = [event async for event in driver.image_edit(request)]

    assert isinstance(events[-1], ChatErrorEvent) and events[-1].code == "upload_unconfirmed"
    assert page.uploaded is not None
    assert page.prompt is None, "an unconfirmed upload must never reach the composer"
    assert context.closed


@pytest.mark.asyncio
async def test_engine_rejects_non_empty_baseline_thread(monkeypatch, tmp_path) -> None:
    blobs = _fast_media(monkeypatch, tmp_path)
    page = FakeMediaPage(baseline_count=2)
    driver, context = _media_driver(page)
    driver.bind_blobs(blobs)
    request = ImageRequest(request_id="gen", account=AccountInfo("acc", "hatch_sess=ok"),
                           model="muse-image", prompt="a red fox")

    events = [event async for event in driver.image_generation(request)]

    assert isinstance(events[-1], ChatErrorEvent) and events[-1].code == "thread_isolation_error"
    assert context.closed


@pytest.mark.asyncio
async def test_engine_downloads_https_media_and_refuses_bad_redirects(monkeypatch, tmp_path) -> None:
    blobs = _fast_media(monkeypatch, tmp_path)
    page = FakeMediaPage(items=[{"src": "https://muse.ai/media/render-1.png", "complete": True,
                                 "width": 512, "height": 512, "alt": ""}])
    driver, context = _media_driver(page)
    driver.bind_blobs(blobs)
    request = ImageRequest(request_id="gen", account=AccountInfo("acc", "hatch_sess=ok"),
                           model="muse-image", prompt="fox")

    events = [event async for event in driver.image_generation(request)]
    assert base64.b64decode(events[-1].images[0]["b64_json"]) == PNG
    assert "https://muse.ai/media/render-1.png" in context.fetched

    evil = FakeMediaPage(items=[{"src": "https://elsewhere.test/render.png", "complete": True,
                                 "width": 512, "height": 512, "alt": ""}])
    driver2, _ctx2 = _media_driver(evil)
    driver2.bind_blobs(blobs)
    events2 = [event async for event in driver2.image_generation(request)]
    assert isinstance(events2[-1], ChatErrorEvent) and events2[-1].code == "media_host_denied"


@pytest.mark.asyncio
async def test_engine_rejects_oversize_media(monkeypatch, tmp_path) -> None:
    blobs = _fast_media(monkeypatch, tmp_path)
    monkeypatch.setattr(image_flow, "DEFAULT_MAX_IMAGE_BYTES", 128)
    page = FakeMediaPage(items=[{"src": "https://muse.ai/media/huge.bin", "complete": True,
                                 "width": 512, "height": 512, "alt": ""}])
    driver, _context = _media_driver(page, media_url="https://muse.ai/media/huge.bin")
    driver.bind_blobs(blobs)
    request = ImageRequest(request_id="gen", account=AccountInfo("acc", "hatch_sess=ok"),
                           model="muse-image", prompt="fox")

    events = [event async for event in driver.image_generation(request)]
    assert isinstance(events[-1], ChatErrorEvent) and events[-1].code == "media_too_large"


@pytest.mark.asyncio
async def test_engine_stores_blob_for_url_mode(monkeypatch, tmp_path) -> None:
    blobs = _fast_media(monkeypatch, tmp_path)
    page = FakeMediaPage()
    driver, _context = _media_driver(page)
    driver.bind_blobs(blobs)
    request = ImageRequest(request_id="gen", account=AccountInfo("acc", "hatch_sess=ok"),
                           model="muse-image", prompt="fox", response_format="url")

    with unittest.mock.patch.dict(os.environ, {"MUSE_MEDIA_BASE_URL": "https://media.example.test"}):
        events = [event async for event in driver.image_generation(request)]

    item = events[-1].images[0]
    assert item["b64_json"] is None and item["media_id"]
    assert item["url"] == f"https://media.example.test/v1/media/{item['media_id']}"
    assert blobs.read(item["media_id"]) == PNG


@pytest.mark.asyncio
async def test_engine_video_job_advances_and_stores_media(monkeypatch, tmp_path) -> None:
    blobs = _fast_media(monkeypatch, tmp_path)
    page = FakeMediaPage()
    driver, context = _media_driver(page, media_url="https://muse.ai/media/out.mp4")
    driver.bind_blobs(blobs)
    request = VideoRequest(request_id="vid", account=AccountInfo("acc", "hatch_sess=ok"),
                           model="muse-video", prompt="a slow orbit", caller_id="caller:me")

    seen = [task.to_dict() async for task in driver.video_generation(request)]

    stages = [task["stage"] for task in seen]
    assert stages.index("opening_thread") < stages.index("waiting_media") < stages.index("completed"), seen
    final = seen[-1]
    assert final["status"] == "completed" and final["progress"] == 100
    assert final["size_bytes"] == len(MOCK_VIDEO_BYTES) and final["content_type"] == "video/mp4"
    assert blobs.read(final["media_id"]) == MOCK_VIDEO_BYTES
    assert context.closed


@pytest.mark.asyncio
async def test_engine_video_job_reports_failure_reason(monkeypatch, tmp_path) -> None:
    blobs = _fast_media(monkeypatch, tmp_path)
    page = FakeMediaPage(baseline_count=1)
    driver, context = _media_driver(page)
    driver.bind_blobs(blobs)
    request = VideoRequest(request_id="vid", account=AccountInfo("acc", "hatch_sess=ok"),
                           model="muse-video", prompt="a slow orbit")

    seen = [task.to_dict() async for task in driver.video_generation(request)]
    assert seen[-1]["status"] == "failed" and seen[-1]["error_code"] == "thread_isolation_error"
    assert context.closed


def test_image_flow_host_allowlist_is_configurable() -> None:
    assert image_flow.host_allowed("https://muse.ai/media/a.png")
    assert image_flow.host_allowed("https://cdn.muse.ai/a.png")
    assert not image_flow.host_allowed("https://muse.ai.cdn-evil.test/a.png")
    assert not image_flow.host_allowed("not a url")
    with unittest.mock.patch.dict(os.environ, {"MUSE_MEDIA_ALLOWED_HOSTS": "cdn.example.test"}):
        assert image_flow.host_allowed("https://cdn.example.test/a.png")
        assert not image_flow.host_allowed("https://muse.ai/a.png")


def test_image_flow_sniffs_only_real_images() -> None:
    assert image_flow.sniff_image_type(PNG) == "image/png"
    assert image_flow.sniff_image_type(b"\xff\xd8\xff\xe0" + b"0" * 20) == "image/jpeg"
    assert image_flow.sniff_image_type(b"RIFF" + b"0" * 4 + b"WEBP" + b"0" * 8) == "image/webp"
    assert image_flow.sniff_image_type(b"RIFF" + b"0" * 4 + b"AAAABBBB") is None
    assert image_flow.sniff_image_type(MOCK_VIDEO_BYTES) is None
    assert image_flow.sniff_image_type(b"tiny") is None


def test_upload_helper_rejects_empty_reference_bytes() -> None:
    async def _run() -> None:
        page = FakeMediaPage()
        with pytest.raises(DriverFailure):
            await image_flow.upload_references(page, [MediaAttachment("e.png", "image/png", "")])

    asyncio.run(_run())
