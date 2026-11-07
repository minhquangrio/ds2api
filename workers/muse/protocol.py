"""Protocol dataclasses and serialization for Muse worker."""

from __future__ import annotations

import json
from dataclasses import asdict, dataclass, field
from typing import Any, Dict, List, Optional


@dataclass
class AccountInfo:
    account_id: str
    cookies: str
    proxy: Optional[str] = None

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> AccountInfo:
        return cls(
            account_id=str(data.get("account_id", "")).strip(),
            cookies=str(data.get("cookies", "")).strip(),
            proxy=data.get("proxy"),
        )

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)


@dataclass
class ChatRequest:
    request_id: str
    account: AccountInfo
    model: str
    prompt: str
    stream: bool = True
    messages: Optional[List[Dict[str, Any]]] = None
    options: Optional[Dict[str, Any]] = field(default_factory=dict)

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> ChatRequest:
        raw_account = data.get("account") or {}
        return cls(
            request_id=str(data.get("request_id", "")).strip(),
            account=AccountInfo.from_dict(raw_account),
            model=str(data.get("model", "muse-spark")).strip(),
            prompt=str(data.get("prompt", "")),
            stream=bool(data.get("stream", True)),
            messages=data.get("messages"),
            options=data.get("options") or {},
        )

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)


@dataclass
class ChatDeltaEvent:
    text_delta: str = ""
    reasoning_delta: str = ""

    def to_dict(self) -> Dict[str, Any]:
        return {
            "type": "delta",
            "text_delta": self.text_delta,
            "reasoning_delta": self.reasoning_delta,
        }

    def to_sse(self) -> str:
        return f"event: delta\ndata: {json.dumps(self.to_dict(), ensure_ascii=False)}\n\n"


@dataclass
class ChatCompletedEvent:
    finish_reason: str = "stop"
    text: str = ""
    reasoning: str = ""
    usage: Optional[Dict[str, Any]] = None
    cookies: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        return {
            "type": "completed",
            "finish_reason": self.finish_reason,
            "text": self.text,
            "reasoning": self.reasoning,
            "usage": self.usage or {},
            "cookies": self.cookies,
        }

    def to_sse(self) -> str:
        return f"event: completed\ndata: {json.dumps(self.to_dict(), ensure_ascii=False)}\n\n"


@dataclass
class ChatErrorEvent:
    error: str
    code: str = "upstream_error"
    status: int = 500

    def to_dict(self) -> Dict[str, Any]:
        return {
            "type": "error",
            "error": self.error,
            "code": self.code,
            "status": self.status,
        }

    def to_sse(self) -> str:
        return f"event: error\ndata: {json.dumps(self.to_dict(), ensure_ascii=False)}\n\n"


@dataclass
class ProbeResult:
    status: str  # "ok", "error", "expired"
    account_id: str
    quota: Optional[Dict[str, Any]] = None
    error: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        return asdict(self)
