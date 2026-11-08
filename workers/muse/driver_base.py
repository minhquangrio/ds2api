"""Abstract base driver interface for Muse.ai worker."""

from __future__ import annotations

import abc
from typing import AsyncIterator, Union

from .media_protocol import (
    ImageCompletedEvent,
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

DriverEvent = Union[ChatDeltaEvent, ChatCompletedEvent, ChatErrorEvent]
ImageDriverEvent = Union[ImageProgressEvent, ImageCompletedEvent, ChatErrorEvent]
VideoDriverEvent = Union[ImageProgressEvent, VideoTask, ChatErrorEvent]


class MuseDriver(abc.ABC):
    """Abstract base class for all Muse driver implementations."""

    @abc.abstractmethod
    async def chat_completion(
        self, request: ChatRequest
    ) -> AsyncIterator[DriverEvent]:
        """Stream chat completion events for the given request."""
        raise NotImplementedError

    @abc.abstractmethod
    async def image_generation(
        self, request: ImageRequest
    ) -> AsyncIterator[ImageDriverEvent]:
        """Generate images from a text prompt only."""
        raise NotImplementedError

    @abc.abstractmethod
    async def image_edit(
        self, request: ImageRequest
    ) -> AsyncIterator[ImageDriverEvent]:
        """Edit the provided reference image(s) following the prompt."""
        raise NotImplementedError

    @abc.abstractmethod
    async def video_generation(
        self, request: VideoRequest
    ) -> AsyncIterator[VideoDriverEvent]:
        """Run a video job to completion, reporting progress then the terminal task."""
        raise NotImplementedError

    @abc.abstractmethod
    async def probe_account(self, account: AccountInfo) -> ProbeResult:
        """Probe account session validity and available quota."""
        raise NotImplementedError

    def bind_blobs(self, blobs) -> None:
        """Attach the worker media blob store used to persist job output."""
        self._blobs = blobs

    def blobs(self):
        """Media blob store, or None when the driver has none bound."""
        return getattr(self, "_blobs", None)

    async def close(self) -> None:
        """Release underlying driver resources."""
        pass
