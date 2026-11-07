"""Abstract base driver interface for Muse.ai worker."""

from __future__ import annotations

import abc
from typing import AsyncIterator, Union

from .protocol import (
    AccountInfo,
    ChatCompletedEvent,
    ChatDeltaEvent,
    ChatErrorEvent,
    ChatRequest,
    ProbeResult,
)

DriverEvent = Union[ChatDeltaEvent, ChatCompletedEvent, ChatErrorEvent]


class MuseDriver(abc.ABC):
    """Abstract base class for all Muse driver implementations."""

    @abc.abstractmethod
    async def chat_completion(
        self, request: ChatRequest
    ) -> AsyncIterator[DriverEvent]:
        """Stream chat completion events for the given request."""
        raise NotImplementedError

    @abc.abstractmethod
    async def probe_account(self, account: AccountInfo) -> ProbeResult:
        """Probe account session validity and available quota."""
        raise NotImplementedError

    async def close(self) -> None:
        """Release underlying driver resources."""
        pass
