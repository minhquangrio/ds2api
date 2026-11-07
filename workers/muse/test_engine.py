"""Offline regressions for real-driver lifecycle, sessions and DOM streaming."""

import asyncio
from contextlib import aclosing

import pytest
from aiohttp.test_utils import TestClient, TestServer

from .browser_session import DriverFailure, parse_cookies, proxy_options
from .driver_engine import EngineMuseDriver, POLL_CHAT_JS, SET_PROMPT_JS, SEND_JS
from .driver_mock import MockMuseDriver
from .protocol import AccountInfo, ChatRequest, ChatCompletedEvent, ChatDeltaEvent, ChatErrorEvent
from .server import create_app


class FakeResponse:
    status = 200

    async def json(self):
        return {"status": "assigned", "vm_id": "fixture", "vm_state": "RUNNING"}


class FakePage:
    url = "https://muse.ai/thread/new"

    def __init__(self, stuck=False):
        self.prompt = None
        self.sent = False
        self.polls = 0
        self.stuck = stuck

    async def goto(self, url, **kwargs):
        self.url = url

    async def wait_for_function(self, script, **kwargs):
        pass

    async def evaluate(self, script, *args):
        if script == SET_PROMPT_JS:
            self.prompt = args[0]
            return True
        if script == SEND_JS:
            self.sent = True
            return True
        assert script == POLL_CHAT_JS
        if not self.sent:
            return {"count": 0, "text": "", "stop": False}
        self.polls += 1
        return {"count": 1, "text": "Hello" if self.polls == 1 else "Hello world", "stop": self.stuck or self.polls < 3}


class FakeContext:
    def __init__(self, page=None):
        self.page = page or FakePage()
        self.request = self
        self.closed = False

    async def add_cookies(self, cookies):
        self.injected = cookies

    async def get(self, url, **kwargs):
        assert url == "https://muse.ai/api/session"
        return FakeResponse()

    async def new_page(self):
        return self.page

    async def cookies(self, url):
        return [{"name": "hatch_sess", "value": "renewed"}]

    async def close(self):
        self.closed = True


class FakeBrowser:
    def __init__(self, context):
        self.context = context

    async def new_context(self, **kwargs):
        self.options = kwargs
        return self.context


@pytest.mark.asyncio
async def test_engine_submits_exact_prompt_and_reads_real_dom():
    context = FakeContext()
    driver = EngineMuseDriver()
    driver._browser = FakeBrowser(context)
    req = ChatRequest("fixture", AccountInfo("acc", "hatch_sess=valid", "http://user:pass@proxy.test:1234"), "muse-spark", "whole prompt\nwith history")
    events = [event async for event in driver.chat_completion(req)]
    assert context.page.prompt == req.prompt and context.page.sent
    assert context.page.url.endswith("/thread/new")
    assert "".join(e.text_delta for e in events if isinstance(e, ChatDeltaEvent)) == "Hello world"
    assert isinstance(events[-1], ChatCompletedEvent) and events[-1].cookies == "hatch_sess=renewed"
    assert driver._browser.options["proxy"] == {"server": "http://proxy.test:1234", "username": "user", "password": "pass"}
    assert context.closed


@pytest.mark.asyncio
async def test_engine_does_not_complete_while_stop_button_is_visible():
    context = FakeContext(FakePage(stuck=True))
    driver = EngineMuseDriver()
    driver._browser = FakeBrowser(context)
    req = ChatRequest("fixture", AccountInfo("acc", "hatch_sess=valid"), "muse-spark", "hello")
    events = []
    async def consume():
        async with aclosing(driver.chat_completion(req)) as stream:
            async for event in stream:
                events.append(event)
    with pytest.raises(TimeoutError):
        async with asyncio.timeout(1.2):
            await consume()
    assert not any(isinstance(e, ChatCompletedEvent) for e in events)
    assert context.closed and not driver.account_lock(req.account).locked()


@pytest.mark.asyncio
async def test_probe_requires_assigned_session():
    context = FakeContext()
    async def rejected(url, **kwargs):
        response = FakeResponse()
        response.status = 401
        return response
    context.get = rejected
    driver = EngineMuseDriver()
    driver._browser = FakeBrowser(context)
    result = await driver.probe_account(AccountInfo("acc", "hatch_sess=invalid"))
    assert result.status == "expired" and result.quota is None
    assert context.closed


def test_cookie_and_proxy_validation():
    for raw in ["anything=fixture", "not_hatch_sess=fixture", "hatch_sess="]:
        with pytest.raises(DriverFailure):
            parse_cookies(raw)
    with pytest.raises(DriverFailure):
        proxy_options("socks5://user:pass@proxy.test:1080")


@pytest.mark.asyncio
async def test_worker_closes_driver_and_rejects_incomplete_nonstream():
    class IncompleteDriver(MockMuseDriver):
        closed = False
        async def chat_completion(self, request):
            yield ChatDeltaEvent(text_delta="partial")
        async def close(self):
            self.closed = True
    driver = IncompleteDriver()
    async with TestClient(TestServer(create_app(driver=driver))) as client:
        response = await client.post("/v1/chat/completions", json={"stream": False})
        assert response.status == 502
        stream = await client.post("/v1/chat/completions", json={"stream": True})
        assert "incomplete_stream" in await stream.text()
    assert driver.closed


def test_default_worker_uses_engine():
    from .server import DRIVER_KEY
    assert isinstance(create_app()[DRIVER_KEY], EngineMuseDriver)


@pytest.mark.asyncio
async def test_client_disconnect_closes_waiting_producer():
    closed = asyncio.Event()
    class WaitingDriver(MockMuseDriver):
        async def chat_completion(self, request):
            try:
                yield ChatDeltaEvent(text_delta="started")
                await asyncio.Event().wait()
            finally:
                closed.set()
    app = create_app(driver=WaitingDriver())
    async with TestClient(TestServer(app, handler_cancellation=True)) as client:
        response = await client.post("/v1/chat/completions", json={"stream": True})
        await response.content.readany()
        response.close()
        await asyncio.wait_for(closed.wait(), timeout=2)
