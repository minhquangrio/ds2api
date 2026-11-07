"""Isolated Playwright sessions, adapted from the pinned MuseEngine session flow."""

from __future__ import annotations

import asyncio
import logging
import uuid
import json
from contextlib import asynccontextmanager
from urllib.parse import unquote, urlsplit

from .protocol import AccountInfo

logger = logging.getLogger("muse_worker.engine")


class DriverFailure(Exception):
    def __init__(self, message: str, code: str = "upstream_error", status: int = 502):
        super().__init__(message)
        self.code, self.status = code, status


def parse_cookies(raw: str) -> list[dict]:
    raw = str(raw or "").strip()
    cookie_map: dict[str, str] = {}
    if raw.startswith("[") or raw.startswith("{"):
        try:
            parsed = json.loads(raw)
            if isinstance(parsed, list):
                for item in parsed:
                    if isinstance(item, dict) and item.get("name") and item.get("value") is not None:
                        cookie_map[str(item["name"]).strip()] = str(item["value"]).strip()
            elif isinstance(parsed, dict):
                for k, v in parsed.items():
                    if k and v is not None:
                        cookie_map[str(k).strip()] = str(v).strip()
        except Exception:
            pass
    if not cookie_map:
        for item in raw.split(";"):
            name, separator, value = item.strip().partition("=")
            if separator and name.strip() and value.strip():
                cookie_map[name.strip()] = value.strip()
    if not cookie_map.get("hatch_sess"):
        raise DriverFailure("hatch_sess session cookie is required", "missing_credentials", 401)
    result = []
    for name, value in cookie_map.items():
        if name.startswith("__Host-"):
            result.append({"name": name, "value": value, "url": "https://muse.ai"})
        else:
            result.append({"name": name, "value": value, "domain": ".muse.ai", "path": "/"})
    return result


def proxy_options(raw: str | None) -> dict:
    if not raw:
        return {}
    parsed = urlsplit(raw)
    if parsed.scheme not in {"http", "https", "socks5"} or not parsed.hostname or not parsed.port:
        raise DriverFailure("invalid account proxy configuration", "proxy_error")
    host = f"[{parsed.hostname}]" if ":" in parsed.hostname else parsed.hostname
    proxy = {"server": f"{parsed.scheme}://{host}:{parsed.port}"}
    if parsed.username is not None:
        if parsed.scheme == "socks5":
            raise DriverFailure("authenticated SOCKS5 requires a proxy relay", "proxy_error")
        proxy.update(username=unquote(parsed.username), password=unquote(parsed.password or ""))
    return {"proxy": proxy}


class BrowserSessions:
    def __init__(self, headless: bool = True, browser_executable_path: str | None = None):
        self.headless = headless
        self.browser_executable_path = browser_executable_path
        self._account_locks: dict[str, asyncio.Lock] = {}
        self._browser_lock = asyncio.Lock()
        self._browser = None
        self._playwright = None
        self._closed = False

    def account_lock(self, account: AccountInfo) -> asyncio.Lock:
        return self._account_locks.setdefault(account.account_id or "default", asyncio.Lock())

    async def _ensure_browser(self) -> None:
        async with self._browser_lock:
            if self._closed:
                raise DriverFailure("worker is shutting down", "worker_unavailable", 503)
            if self._browser is not None:
                return
            try:
                from playwright.async_api import async_playwright
                self._playwright = await async_playwright().start()
                self._browser = await self._playwright.chromium.launch(
                    headless=self.headless, executable_path=self.browser_executable_path,
                    args=["--disable-blink-features=AutomationControlled"],
                )
            except asyncio.CancelledError:
                await self._close_resource(self._playwright, "stop")
                self._playwright = None
                raise
            except Exception:
                await self._close_resource(self._playwright, "stop")
                self._playwright = None
                raise DriverFailure("Chromium runtime could not start", "browser_runtime_unavailable", 503) from None

    @staticmethod
    async def _close_resource(resource, method: str = "close") -> None:
        if resource is not None:
            try:
                await getattr(resource, method)()
            except Exception as exc:
                logger.warning("Browser cleanup failed (%s)", type(exc).__name__)

    @staticmethod
    async def _session_state(context) -> dict:
        response = await context.request.get("https://muse.ai/api/session", timeout=15000, max_redirects=0)
        if response.status == 401:
            raise DriverFailure("Muse session expired or rejected", "session_expired", 401)
        if response.status == 429:
            raise DriverFailure("Muse account rate limited", "rate_limit_exceeded", 429)
        if response.status != 200:
            raise DriverFailure(f"Muse session check returned HTTP {response.status}")
        state = await response.json()
        if not isinstance(state, dict) or state.get("status") != "assigned":
            raise DriverFailure("Muse session assignment was not confirmed", "session_unavailable", 503)
        return state

    @asynccontextmanager
    async def session(self, account: AccountInfo, wake: bool = False):
        cookies = parse_cookies(account.cookies)
        options = proxy_options(account.proxy)
        await self._ensure_browser()
        context = await self._browser.new_context(**options)
        try:
            await context.add_cookies(cookies)
            state = await self._session_state(context)
            if wake and state.get("vm_state") != "RUNNING":
                if not state.get("vm_id") or state.get("vm_state") == "DISABLED":
                    raise DriverFailure("Muse workspace is unavailable", "vm_unavailable", 503)
                response = await context.request.post("https://muse.ai/api/hatch/vm/wake", timeout=10000, max_redirects=0,
                    data={"vm_id": state["vm_id"], "retry_count": 0, "connect_attempt_id": str(uuid.uuid4())})
                if response.status != 200:
                    raise DriverFailure(f"Muse wake request returned HTTP {response.status}", "vm_unavailable", 503)
                for _ in range(20):
                    state = await self._session_state(context)
                    if state.get("vm_state") == "RUNNING":
                        break
                    await asyncio.sleep(0.5)
                else:
                    raise DriverFailure("Muse VM did not reach RUNNING", "vm_unavailable", 503)
            yield context, state
        finally:
            await self._close_resource(context)

    async def close(self) -> None:
        async with self._browser_lock:
            self._closed = True
            await self._close_resource(self._browser)
            await self._close_resource(self._playwright, "stop")
            self._browser = self._playwright = None
