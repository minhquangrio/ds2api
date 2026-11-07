# Muse worker

The worker executes Muse text chat through Playwright. DS2API owns prompt
normalization, tool parsing, history, usage and account switching. Engine is the
default; `mock` is an explicit offline test option.

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
```

On Vercel the worker runs on a separate host; the functions proxy to it. Custom
model aliases are resolved by Go before any DeepSeek account/PoW preparation.

## Supported behavior

- `muse-spark` and `muse-chat` use the current Muse web model; these names do not
  select independent upstream model versions.
- Each chat/probe uses the account lock and an isolated browser context. Each
  chat opens a fresh thread and submits the full Go-prepared prompt.
- Session probes require `/api/session` to confirm `assigned`. Chat wakes the VM
  if needed and requires `RUNNING`. The driver does not invent account quotas.
- HTTP/HTTPS and unauthenticated SOCKS5 proxies are supported. Authenticated
  SOCKS5 requires a separately configured relay; it never falls back to direct.
- Stream failure/cancellation closes the producer/context. Generation remains
  ongoing while the Stop button is visible; timeout yields an error.
- Text files are handled in Go. Image/video generation and vision attachments
  are outside this worker's current chat implementation.

## Verification

```powershell
python -m pip install -r workers/muse/requirements-dev.txt
python -m pytest workers/muse -q
```

Offline tests cover RPC, real-driver session/DOM fixtures, proxy forwarding,
output completion, cancellation and cleanup. They do not contact Muse. No live
account test was performed during the fix; validate a session-only account,
multi-turn prompt, stream cancellation and the chosen proxy on the worker host
before using this integration in production.

Source snapshots and the preserved engine license are recorded in [UPSTREAM.md](UPSTREAM.md).
