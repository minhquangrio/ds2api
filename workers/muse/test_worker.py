"""Tests for Muse worker protocol, mock driver, and server endpoints."""

from __future__ import annotations

import asyncio
import json
import pytest
from aiohttp import web
from aiohttp.test_utils import AioHTTPTestCase

from .driver_mock import MockMuseDriver
from .protocol import (
    AccountInfo,
    ChatCompletedEvent,
    ChatDeltaEvent,
    ChatErrorEvent,
    ChatRequest,
    ProbeResult,
)
from .server import create_app


@pytest.mark.asyncio
async def test_protocol_serialization() -> None:
    acc = AccountInfo(account_id="acc_1", cookies="hatch_sess=abc", proxy="http://127.0.0.1:8080")
    d = acc.to_dict()
    assert d["account_id"] == "acc_1"
    assert d["cookies"] == "hatch_sess=abc"
    assert d["proxy"] == "http://127.0.0.1:8080"

    reconstructed = AccountInfo.from_dict(d)
    assert reconstructed.account_id == "acc_1"
    assert reconstructed.cookies == "hatch_sess=abc"

    req = ChatRequest(
        request_id="req_123",
        account=acc,
        model="muse-spark",
        prompt="Hello",
        stream=True,
    )
    req_dict = req.to_dict()
    assert req_dict["request_id"] == "req_123"
    assert req_dict["stream"] is True

    delta_ev = ChatDeltaEvent(text_delta="chunk", reasoning_delta="think")
    assert "event: delta" in delta_ev.to_sse()
    assert "chunk" in delta_ev.to_sse()

    comp_ev = ChatCompletedEvent(finish_reason="stop", text="Done")
    assert "event: completed" in comp_ev.to_sse()

    err_ev = ChatErrorEvent(error="failed", code="fail_code", status=400)
    assert "event: error" in err_ev.to_sse()


@pytest.mark.asyncio
async def test_mock_driver_chat_stream() -> None:
    driver = MockMuseDriver(chunk_delay=0.001)
    acc = AccountInfo(account_id="test_acc", cookies="hatch_sess=valid")
    req = ChatRequest(
        request_id="req_1",
        account=acc,
        model="muse-spark",
        prompt="[reply:Hello world from Muse] test prompt",
        stream=True,
    )

    events = []
    async for event in driver.chat_completion(req):
        events.append(event)

    # Should have deltas and a completed event
    assert len(events) >= 2
    completed = [e for e in events if isinstance(e, ChatCompletedEvent)]
    assert len(completed) == 1
    assert completed[0].text == "Hello world from Muse"
    assert completed[0].finish_reason == "stop"
    assert completed[0].usage is not None
    assert completed[0].usage["prompt_tokens"] > 0


@pytest.mark.asyncio
async def test_mock_driver_reasoning() -> None:
    driver = MockMuseDriver(chunk_delay=0.001)
    acc = AccountInfo(account_id="test_acc", cookies="hatch_sess=valid")
    req = ChatRequest(
        request_id="req_2",
        account=acc,
        model="muse-spark",
        prompt="[trigger:reasoning:deep thought here][reply:Result text]",
        stream=True,
    )

    deltas = []
    reasoning_deltas = []
    async for event in driver.chat_completion(req):
        if isinstance(event, ChatDeltaEvent):
            if event.text_delta:
                deltas.append(event.text_delta)
            if event.reasoning_delta:
                reasoning_deltas.append(event.reasoning_delta)

    assert len(reasoning_deltas) > 0
    assert "deep " in "".join(reasoning_deltas)
    assert "".join(deltas) == "Result text"


@pytest.mark.asyncio
async def test_mock_driver_errors() -> None:
    driver = MockMuseDriver(chunk_delay=0.001)

    # Missing cookies
    acc_no_cookie = AccountInfo(account_id="test_acc", cookies="")
    req1 = ChatRequest(request_id="r1", account=acc_no_cookie, model="muse-spark", prompt="hi")
    events1 = [e async for e in driver.chat_completion(req1)]
    assert len(events1) == 1
    assert isinstance(events1[0], ChatErrorEvent)
    assert events1[0].status == 401

    # Throttled trigger
    acc = AccountInfo(account_id="test_acc", cookies="hatch_sess=valid")
    req2 = ChatRequest(request_id="r2", account=acc, model="muse-spark", prompt="[trigger:throttle]")
    events2 = [e async for e in driver.chat_completion(req2)]
    assert len(events2) == 1
    assert isinstance(events2[0], ChatErrorEvent)
    assert events2[0].status == 429
    assert events2[0].code == "rate_limit_exceeded"

    # Custom error trigger
    req3 = ChatRequest(
        request_id="r3",
        account=acc,
        model="muse-spark",
        prompt="[trigger:error:403:forbidden_action:access denied]",
    )
    events3 = [e async for e in driver.chat_completion(req3)]
    assert len(events3) == 1
    assert isinstance(events3[0], ChatErrorEvent)
    assert events3[0].status == 403
    assert events3[0].code == "forbidden_action"


@pytest.mark.asyncio
async def test_mock_driver_probe_account() -> None:
    driver = MockMuseDriver()

    # Valid
    p1 = await driver.probe_account(AccountInfo("a1", "hatch_sess=valid"))
    assert p1.status == "ok"
    assert p1.quota is not None

    # Missing
    p2 = await driver.probe_account(AccountInfo("a2", ""))
    assert p2.status == "error"

    # Expired
    p3 = await driver.probe_account(AccountInfo("a3", "invalid_cookie"))
    assert p3.status == "expired"


@pytest.mark.asyncio
async def test_mock_driver_account_concurrency_lock() -> None:
    """Verifies that requests for the same account are physically serialized."""
    driver = MockMuseDriver(chunk_delay=0.02)
    acc = AccountInfo(account_id="locked_acc", cookies="hatch_sess=valid")

    req1 = ChatRequest(request_id="r1", account=acc, model="muse-spark", prompt="first")
    req2 = ChatRequest(request_id="r2", account=acc, model="muse-spark", prompt="second")

    order = []

    async def run_req(req: ChatRequest, name: str) -> None:
        async for event in driver.chat_completion(req):
            if isinstance(event, ChatCompletedEvent):
                order.append(name)

    # Launch both concurrently
    await asyncio.gather(run_req(req1, "req1"), run_req(req2, "req2"))

    # Both completed cleanly
    assert len(order) == 2


class ServerTestCase(AioHTTPTestCase):
    async def get_application(self) -> web.Application:
        self.mock_driver = MockMuseDriver(chunk_delay=0.001)
        return create_app(driver=self.mock_driver, secret="test-secret-123")

    async def test_health_endpoint(self) -> None:
        resp = await self.client.request("GET", "/health")
        assert resp.status == 200
        data = await resp.json()
        assert data["status"] == "ok"
        assert data["driver"] == "MockMuseDriver"

    async def test_auth_rejection(self) -> None:
        resp = await self.client.request(
            "POST",
            "/v1/chat/completions",
            json={"prompt": "hi"},
            headers={"X-Worker-Secret": "wrong-secret"},
        )
        assert resp.status == 401

    async def test_nonstream_chat(self) -> None:
        headers = {"X-Worker-Secret": "test-secret-123"}
        payload = {
            "request_id": "test_req",
            "account": {"account_id": "acc_1", "cookies": "hatch_sess=xyz"},
            "model": "muse-spark",
            "prompt": "[reply:Exact non-stream result]",
            "stream": False,
        }
        resp = await self.client.request(
            "POST",
            "/v1/chat/completions",
            json=payload,
            headers=headers,
        )
        assert resp.status == 200
        data = await resp.json()
        assert data["status"] == "ok"
        assert data["text"] == "Exact non-stream result"
        assert data["finish_reason"] == "stop"

    async def test_streaming_chat(self) -> None:
        headers = {"Authorization": "Bearer test-secret-123"}
        payload = {
            "request_id": "test_req_stream",
            "account": {"account_id": "acc_1", "cookies": "hatch_sess=xyz"},
            "model": "muse-spark",
            "prompt": "[reply:Hello streamed]",
            "stream": True,
        }
        resp = await self.client.request(
            "POST",
            "/v1/chat/completions",
            json=payload,
            headers=headers,
        )
        assert resp.status == 200
        assert "text/event-stream" in resp.headers["Content-Type"]

        body_text = await resp.text()
        assert "event: delta" in body_text
        assert "event: completed" in body_text

    async def test_probe_endpoint(self) -> None:
        headers = {"X-Worker-Secret": "test-secret-123"}
        payload = {"account_id": "acc_1", "cookies": "hatch_sess=ok"}
        resp = await self.client.request("POST", "/v1/account/probe", json=payload, headers=headers)
        assert resp.status == 200
        data = await resp.json()
        assert data["status"] == "ok"
