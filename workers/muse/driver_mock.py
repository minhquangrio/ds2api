"""Mock driver for offline testing and CI verification without Chromium."""

from __future__ import annotations

import asyncio
from typing import AsyncIterator, Dict

from .driver_base import DriverEvent, MuseDriver
from .protocol import (
    AccountInfo,
    ChatCompletedEvent,
    ChatDeltaEvent,
    ChatErrorEvent,
    ChatRequest,
    ProbeResult,
)


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
