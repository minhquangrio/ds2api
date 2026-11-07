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
  - StandardRequest / AssistantTurn normalization.
  - Tool injection and tool-call detection.
  - Chat history persistence, quota enforcement, and usage ledger.
  - Multi-protocol adapters (OpenAI Chat, OpenAI Responses, Claude, Gemini).
  - Account selection, cooldown, and retry policies.

- **Python Worker (`workers/muse/`):**
  - Physical browser automation lifecycle via Chromium / CDP.
  - Per-account serialized execution lock (1 concurrent request per Muse account).
  - DOM event capture and token/delta streaming to Go.
  - Upstream session cookies validation and renewal reporting.
  - Cancellation handling that stops browser tasks and releases worker slots.

## 3. License Notice

Both upstream projects are licensed under the MIT License.
Original engine copyright: Copyright (c) 2026 MUSE2API Authors.
The upstream license is preserved verbatim in [UPSTREAM_LICENSE](UPSTREAM_LICENSE).
Modifications, adaptations, and worker RPC layer: Copyright (c) DS2API Authors.
Licensed under the MIT License.
