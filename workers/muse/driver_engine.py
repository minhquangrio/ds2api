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
from .driver_base import MuseDriver, DriverEvent
from .protocol import AccountInfo, ChatCompletedEvent, ChatDeltaEvent, ChatErrorEvent, ChatRequest, ProbeResult

logger = logging.getLogger("muse_worker.engine")

READY_JS = """() => {
    const ta = document.querySelector('textarea');
    const shell = document.querySelector('[data-hatch-shell-hydration-state]');
    return document.readyState === 'complete' && ta && !ta.disabled &&
        (!shell || shell.getAttribute('data-hatch-shell-hydration-state') === 'hydrated') &&
        !document.body.innerText.includes('Connecting...');
}"""

SET_PROMPT_JS = """text => {
    const ta = document.querySelector('textarea');
    ta.focus();
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(ta, text);
    ta.dispatchEvent(new Event('input', {bubbles:true}));
    ta.dispatchEvent(new Event('change', {bubbles:true}));
    return ta.value === text;
}"""

SEND_JS = """() => {
    const button = [...document.querySelectorAll('button,[role=button]')]
        .filter(b => b.offsetParent !== null)
        .find(b => /发送|send/i.test(b.getAttribute('aria-label') || '') ||
            /发送|send/i.test(b.getAttribute('data-testid') || '') || /send/i.test(b.innerText || ''));
    if (!button || button.disabled) return false;
    button.click(); return true;
}"""

POLL_CHAT_JS = """() => {
    const scope = document.querySelector('main,[class*="chat-scroll"],[class*="hatch-chat-scroll"]') || document.body;
    const bubbles = [...scope.querySelectorAll('div[class*="hatch-chat-groupable-bubble"]')]
        .filter(b => /hatch-agent-bubble-bg/.test(b.className || ''));
    const last = bubbles.length ? bubbles[bubbles.length-1] : null;
    if (last) last.scrollIntoView({block:'end'});
    const stop = !!document.querySelector('[data-testid="hatch-composer-stop-button"],button[aria-label*="Stop" i],button[aria-label*="停止"]');
    return {count:bubbles.length, text:last ? (last.innerText || '').trim() : '', stop};
}"""


class EngineMuseDriver(BrowserSessions, MuseDriver):
    async def chat_completion(self, request: ChatRequest) -> AsyncIterator[DriverEvent]:
        if request.model not in {"muse-spark", "muse-chat", "spark", "chat"}:
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
