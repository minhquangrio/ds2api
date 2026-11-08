# Upstream Attribution & Provenance for Muse Worker

This directory contains the Python worker service for Muse.ai integration in DS2API.

## 1. Upstream Sources

| Source | Snapshot / Commit | License | Borrowed / Adapted Components | Notes & Limitations |
| --- | --- | --- | --- | --- |
| [www222fff/muse2api](https://github.com/www222fff/muse2api) | `3a20b5e7aa4056c99c0eb8c732d73ce3804d5dbf` | MIT | Driver interface `MuseDriver`, event models, mock driver fixtures, stream/cancel semantics | Direct HTTP driver in upstream does not implement chat/media yet; browser driver requires real credentials. |
| [czg86389-hub/muse2api](https://github.com/czg86389-hub/muse2api) | `8a3dab9db13b23ac694d21af29e65504bc7e69b1` | MIT | `engine.py` session assignment/wake flow, composer setter/send selectors and assistant-bubble polling, adapted to asynchronous Playwright in `browser_session.py` and `driver_engine.py` | Every request uses an isolated context and a new thread. No warm conversation reuse or early completion while Stop remains visible. Prompt/tools/usage stay in Go. Offline DOM/session fixtures pass; live Muse verification remains required. |

## 2. Architecture Boundary

- **Go (DS2API Core):**
  - Caller authentication, model resolution and aliases.
  - StandardRequest / AssistantTurn normalization for text, and `internal/mediacompat`
    normalization for image and video requests (media results never become a Turn).
  - Tool injection and tool-call detection.
  - Chat history persistence, quota enforcement, and usage ledger.
  - Multi-protocol adapters (OpenAI Chat, OpenAI Responses, Claude, Gemini, OpenAI Images/Videos).
  - Account selection, cooldown, and retry policies.

- **Python Worker (`workers/muse/`):**
  - Physical browser automation lifecycle via Chromium / CDP.
  - Per-account serialized execution lock (1 concurrent request per Muse account).
  - DOM event capture and token/delta streaming to Go.
  - Upstream session cookies validation and renewal reporting.
  - Cancellation handling that stops browser tasks and releases worker slots.

## 2b. Media Provenance (images and video)

| Component | Provenance | Verification status |
| --- | --- | --- |
| `chat_js.py` composer/send/agent-bubble selectors (`READY_JS`, `SET_PROMPT_JS`, `SEND_JS`, `POLL_CHAT_JS`) | Adapted from the pinned czg86389-hub engine, moved verbatim so media flows reuse one contract | Offline fixtures pass; live chat path unchanged by this move |
| `media_js.py` (`FILE_INPUT_SELECTOR_JS`, `UPLOAD_PREVIEW_JS`, `POLL_IMAGE_JS`, `POLL_VIDEO_JS`, `FETCH_MEDIA_B64_JS`) | **DS2API-original.** Written from the DOM contract the chat driver already relies on (agent bubbles matching `hatch-agent-bubble-bg`, composer `textarea`, Stop button test ids) plus generic HTML file-input behaviour. Not copied from either upstream repository | **Not verified against a live Muse.ai account.** Proven only by offline fakes in `test_media.py`; run `scripts/muse_media_live_check.py` with real credentials before publishing the capability |
| `image_flow.py`, `video_flow.py` (upload acknowledgement, only-new-media rule, host/MIME/size validation, cookie refresh) | DS2API-original, written to the contract in `plan/muse2api_integration.md` ("trả đúng media mới của lần sinh hiện tại; reference upload được xác nhận") | Offline; live unverified |
| `media_protocol.py`, `job_store.py`, `server_media.py`, `http_common.py`, `mock_media.py` | DS2API-original worker RPC, durable job/media store and offline fixtures. The event/request *shape* follows the www222fff event-model pattern already credited above | Offline tests: `python -m pytest workers/muse -q` |

The mock driver's video payload (`mock_media.MOCK_VIDEO_BYTES`) is an opaque fixture, not a playable container; it proves transport, ownership and polling only.

## 3. License Notice

Both upstream projects are licensed under the MIT License.
Original engine copyright: Copyright (c) 2026 MUSE2API Authors.
The upstream license is preserved verbatim in [UPSTREAM_LICENSE](UPSTREAM_LICENSE).
Modifications, adaptations, and worker RPC layer: Copyright (c) DS2API Authors.
Licensed under the MIT License.
