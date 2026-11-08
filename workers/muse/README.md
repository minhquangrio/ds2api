# Muse worker

The worker executes Muse text chat, image generation, reference-image edits and
video jobs through Playwright. DS2API owns prompt normalization, tool parsing,
history, usage, auth/quota and account switching. Engine is the default; `mock`
is an explicit offline test option.

## Local setup (from the repository root)

```powershell
python -m venv .venv-muse
.venv-muse\Scripts\python -m pip install -r workers/muse/requirements.txt
.venv-muse\Scripts\python -m playwright install chromium
$env:MUSE_WORKER_SECRET = "replace-with-service-secret"
.venv-muse\Scripts\python -m workers.muse.server --driver engine
```

Use the same secret in DS2API's `muse.secret` / `DS2API_MUSE_WORKER_SECRET`.
Set `muse.worker_url` / `DS2API_MUSE_WORKER_URL` to the worker HTTP address.
Configure an account with `provider: "muse"`, a name and its `hatch_sess` cookie.
Only configured DS2API API keys can call Muse. Keep the worker on a private
network, or protect remote access with the service secret and HTTPS.

```powershell
docker build -f workers/muse/Dockerfile -t ds2api-muse-worker .
docker run --rm -p 127.0.0.1:8765:8765 -e MUSE_WORKER_SECRET ds2api-muse-worker
# Media output must live on a writable volume, or jobs lose their bytes on restart:
#   -v ./data/muse_media:/app/data/muse_media
```

On Vercel the worker runs on a separate host; the functions proxy to it. Custom
model aliases are resolved by Go before any DeepSeek account/PoW preparation.

## RPC surface

| Route | Purpose | Response |
| --- | --- | --- |
| `GET /health`, `GET /v1/health` | driver and version probe, no auth | JSON |
| `POST /v1/chat/completions` | text chat | SSE (`delta`/`completed`/`error`) or JSON when `stream=false` |
| `POST /v1/images/generations` | text to image | single-shot JSON `{created, data:[{b64_json\|url, media_id, content_type, width, height}]}` |
| `POST /v1/images/edits` | reference image(s) + instruction | same envelope; `references[]` are required |
| `POST /v1/videos` | submit an async job | `{task:{id,status,stage,progress,...}}`, job runs in the background |
| `GET /v1/videos/{id}` / `GET /v1/videos` | poll one / list this caller's jobs | task JSON; a foreign `caller_id` is 404 |
| `GET /v1/videos/{id}/content` | finished bytes | video bytes with `Content-Disposition` |
| `GET /v1/media/{media_id}` | stored image bytes for `response_format=url` | image bytes (gateway enforces caller ownership) |
| `POST /v1/account/probe` | session validity | JSON, quota only from `/api/session` |

Media requests carry `caller_id`; the worker keeps the job record and the blob,
so polling survives restarts and one caller cannot read another's task. Image
responses are never streamed, and media is never retried automatically: a
timeout after submission is reported as a failure because upstream may already
have billed the generation.

## Media environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `MUSE_MEDIA_DIR` | `./data/muse_media` | blob + job store location |
| `MUSE_MEDIA_TTL_HOURS` | `24` | purge age for stored media and terminal jobs |
| `MUSE_MEDIA_MAX_BYTES` | `67108864` | per-blob write cap |
| `MUSE_IMAGE_MAX_BYTES` | `8388608` | per-image download cap (b64 must stay under the Go line limit) |
| `MUSE_VIDEO_MAX_BYTES` | `67108864` | per-video download cap |
| `MUSE_IMAGE_MAX_N` | `4` | images per request |
| `MUSE_REFERENCE_MAX_BYTES` | `16777216` | accepted reference size |
| `MUSE_IMAGE_TIMEOUT` / `MUSE_VIDEO_TIMEOUT` | `240` / `900` | per-operation deadlines |
| `MUSE_MEDIA_ALLOWED_HOSTS` | `muse.ai` | hosts allowed to serve generated media (redirect checked too) |
| `MUSE_MEDIA_BASE_URL` | unset | absolute base for `url` responses; unset keeps `b64_json` |

## Mock driver knobs (offline only)

The mock never opens a browser and is selected with `--driver mock`:

- `[reply:TEXT]`, `[trigger:reasoning:X]`, `[trigger:cookie:X]`,
  `[trigger:throttle]`, `[trigger:error:STATUS:CODE:MSG]` — chat as before.
- `[image:n=N]` — return N images; `[trigger:no_media]` — complete with no
  media (the error path); `[trigger:upload_fail]` — reference upload is never
  acknowledged; `[trigger:ref_hash]` — echo the SHA-256 of the first reference
  so a test can prove the bytes reached the worker.
- `[video:fail]` — the video job records a failure reason.
- Image bytes come from `mock_media.build_png_bytes()`: a real, stdlib-built PNG
  with a fixed SHA-256. The video payload is an opaque fixture, **not** a
  playable file — it tests transport, ownership and polling only.

## Supported behavior

- `muse-spark` and `muse-chat` use the current Muse web model; `muse-image` and
  `muse-video` select the image and video transports. These names do not choose
  independent upstream model versions, and each transport rejects the others'
  model kinds.
- Each chat/probe uses the account lock and an isolated browser context. Each
  chat and media operation opens a fresh thread and submits the full Go-prepared prompt.
- An image edit only proceeds after the upload is visibly acknowledged inside the
  composer; otherwise it fails as `upload_unconfirmed` and never submits.
- Results are read from the last agent bubble of the fresh thread only, so a
  reference preview or an older attachment can never be returned as the output.
- Session probes require `/api/session` to confirm `assigned`. Chat wakes the VM
  if needed and requires `RUNNING`. The driver does not invent account quotas.
- HTTP/HTTPS and unauthenticated SOCKS5 proxies are supported. Authenticated
  SOCKS5 requires a separately configured relay; it never falls back to direct.
- Stream failure/cancellation closes the producer/context. Generation remains
  ongoing while the Stop button is visible; timeout yields an error.
- Text files are handled in Go. Vision attachments on the chat transport stay
  unsupported and still return an explicit error.

## Verification

```powershell
python -m pip install -r workers/muse/requirements-dev.txt
python -m pytest workers/muse -q
```

Offline tests cover RPC, the media contract (validation, blob store, restart
durability, caller ownership), real-driver session/DOM fixtures, proxy
forwarding, output completion, cancellation and cleanup. They do not contact Muse.

**The media selectors are not verified against a live Muse.ai account.** The
attach control, upload acknowledgement, and the in-bubble `img`/`video` reads are
inferred from the DOM contract the chat driver already uses. Before publishing
image or video capability, run the opt-in live check on the worker host:

```powershell
$env:MUSE_LIVE_ACCOUNT_COOKIE = "hatch_sess=..."   # never logged or committed
python scripts/muse_media_live_check.py --mode generation
python scripts/muse_media_live_check.py --mode edit --image path/to/ref.png --prompt "make it night"
python scripts/muse_media_live_check.py --mode video
```

Also validate a session-only account, multi-turn prompt, stream cancellation and
the chosen proxy on the worker host before using this integration in production.

Source snapshots and the preserved engine license are recorded in [UPSTREAM.md](UPSTREAM.md).
