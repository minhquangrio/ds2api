#!/usr/bin/env python3
"""Opt-in live verification for the Muse media capability.

The offline suite (``python -m pytest workers/muse -q``) proves the control flow
against scripted DOM fakes. It cannot prove that muse.ai actually exposes an
image upload control, acknowledges the upload, or renders the result inside the
last agent bubble. This script runs the real browser driver against a real
account and prints which of those steps actually work.

It is deliberately opt-in and credential-safe:

* It refuses to run unless the cookie is supplied through an environment
  variable. The cookie value is never printed, logged, written to a report, or
  accepted as a command-line argument (arguments end up in shell history).
* It writes generated media to --out-dir and nowhere else.
* A non-zero exit means at least one step failed, which is the signal not to
  publish the capability.

Usage (from the repository root, on the worker host, with the same Playwright
environment the worker uses):

    $env:MUSE_LIVE_ACCOUNT_COOKIE = "hatch_sess=..."      # PowerShell
    python scripts/muse_media_live_check.py --mode generation
    python scripts/muse_media_live_check.py --mode edit --image path/to/ref.png --prompt "make it night"
    python scripts/muse_media_live_check.py --mode video --timeout 900

Exit codes: 0 = every probed step produced media; 1 = a step failed; 2 = the
script was not usable (missing cookie, missing image file, or Playwright absent).
"""

from __future__ import annotations

import argparse
import asyncio
import base64
import os
import sys
import time
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from workers.muse.driver_engine import EngineMuseDriver  # noqa: E402
from workers.muse.media_protocol import ImageRequest, MediaAttachment, VideoRequest  # noqa: E402
from workers.muse.protocol import AccountInfo, ChatErrorEvent  # noqa: E402

COOKIE_ENV = "MUSE_LIVE_ACCOUNT_COOKIE"
DEFAULT_PROMPT = "a small red fox sitting on a rock, studio lighting"
DEFAULT_PROXY_ENV = "MUSE_LIVE_ACCOUNT_PROXY"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Verify Muse.ai media capability with a real account.")
    parser.add_argument("--mode", choices=("generation", "edit", "video"), default="generation")
    parser.add_argument("--prompt", default=DEFAULT_PROMPT)
    parser.add_argument("--image", type=Path, help="reference image file (required for --mode edit)")
    parser.add_argument("--account-cookie-env", default=COOKIE_ENV,
                        help="name of the environment variable holding the hatch_sess cookie (default: %(default)s)")
    parser.add_argument("--proxy-env", default=DEFAULT_PROXY_ENV,
                        help="optional environment variable holding the account proxy URL")
    parser.add_argument("--out-dir", type=Path, default=Path(".tmp/muse-media-live"),
                        help="where to write the fetched media (default: %(default)s)")
    parser.add_argument("--n", type=int, default=1, help="images to request in generation mode")
    parser.add_argument("--timeout", type=float, default=900.0, help="overall budget in seconds")
    return parser.parse_args()


def mask(value: str) -> str:
    if not value:
        return "<empty>"
    if len(value) <= 12:
        return "*" * len(value)
    return f"{value[:6]}...{value[-4:]} ({len(value)} chars)"


async def run_images(driver: EngineMuseDriver, account: AccountInfo, args: argparse.Namespace) -> int:
    references = []
    if args.mode == "edit":
        data = args.image.read_bytes()
        references = [MediaAttachment(filename=args.image.name, content_type="image/png",
                                      data_base64=base64.b64encode(data).decode("ascii"))]
    request = ImageRequest(
        request_id=f"livecheck_{int(time.time())}",
        account=account,
        model="muse-image",
        prompt=args.prompt,
        mode="edit" if args.mode == "edit" else "generation",
        n=args.n,
        response_format="b64_json",
        caller_id="live-check",
        references=references,
    )
    events = []
    async for event in (driver.image_edit(request) if args.mode == "edit" else driver.image_generation(request)):
        events.append(event)
    return report_images(events, args.out_dir, args.mode)


def report_images(events, out_dir: Path, mode: str) -> int:
    from workers.muse.media_protocol import ImageCompletedEvent  # local import keeps the top block short

    for event in events:
        if isinstance(event, ImageCompletedEvent):
            out_dir.mkdir(parents=True, exist_ok=True)
            print(f"[ok] {mode} produced {len(event.images)} image(s)")
            if event.thread_url:
                print(f"     thread: {event.thread_url}")
            for index, item in enumerate(event.images):
                payload = base64.b64decode(item.get("b64_json") or "")
                target = out_dir / f"{mode}-{index}.img"
                target.write_bytes(payload)
                print(f"     wrote {target} ({len(payload)} bytes, {item.get('content_type')}, "
                      f"{item.get('width')}x{item.get('height')})")
            return 0
        if isinstance(event, ChatErrorEvent):
            print(f"[FAIL] {event.code} (HTTP {event.status}): {event.error}")
            print("       The selectors in workers/muse/media_js.py do not match the current muse.ai DOM.")
            return 1
    print("[FAIL] driver ended without a completed or error event")
    return 1


async def run_video(driver: EngineMuseDriver, account: AccountInfo, args: argparse.Namespace) -> int:
    from workers.muse.job_store import JobRegistry

    store = JobRegistry(args.out_dir / "jobs")
    driver.bind_blobs(store.blobs)
    request = VideoRequest(
        request_id=f"livecheck_{int(time.time())}",
        account=account,
        model="muse-video",
        prompt=args.prompt if args.prompt != DEFAULT_PROMPT else "a slow orbit around a glass sphere",
        caller_id="live-check",
    )
    tasks = []
    async for task in driver.video_generation(request):
        tasks.append(task)
        print(f"     stage={task.stage} status={task.status} progress={task.progress}")
    final = tasks[-1] if tasks else None
    if final is None:
        print("[FAIL] video driver produced no task events")
        return 1
    if final.status != "completed" or not final.media_id:
        print(f"[FAIL] video job ended as {final.status}: {final.error} (code={final.error_code})")
        return 1
    out_dir = args.out_dir
    out_dir.mkdir(parents=True, exist_ok=True)
    payload = store.blobs.read(final.media_id)
    target = out_dir / "video-check.mp4"
    target.write_bytes(payload or b"")
    print(f"[ok] video completed: {target} ({len(payload or b'')} bytes, {final.content_type})")
    return 0 if payload else 1


async def main_async(args: argparse.Namespace) -> int:
    cookie = os.environ.get(args.account_cookie_env, "").strip()
    if not cookie:
        print(f"[setup] no cookie found in ${args.account_cookie_env}; refusing to run live checks.")
        print("        Set it in the environment first (never as a command-line argument).")
        return 2
    if args.mode == "edit" and (args.image is None or not args.image.exists()):
        print("[setup] --mode edit needs --image pointing at an existing file")
        return 2

    proxy = os.environ.get(args.proxy_env, "").strip() or None
    account = AccountInfo(account_id="live-check", cookies=cookie, proxy=proxy)
    print(f"[setup] account cookie {mask(cookie)}; proxy {proxy.split('@')[-1] if proxy else '<direct>'}")
    print(f"[setup] mode={args.mode} timeout={args.timeout:.0f}s out={args.out_dir}")

    driver = EngineMuseDriver()
    try:
        if args.mode == "video":
            return await asyncio.wait_for(run_video(driver, account, args), timeout=args.timeout)
        return await asyncio.wait_for(run_images(driver, account, args), timeout=args.timeout)
    except asyncio.TimeoutError:
        print(f"[FAIL] no result within {args.timeout:.0f}s (a real upstream job may still have been billed)")
        return 1
    except Exception as exc:  # noqa: BLE001 - the point of a live check is to surface anything unexpected
        name = type(exc).__name__
        if "playwright" in name.lower() or "Executable doesn't exist" in str(exc):
            print("[setup] Playwright Chromium is not installed here. Run:")
            print("        python -m playwright install chromium")
            return 2
        print(f"[FAIL] {name}: {exc}")
        return 1
    finally:
        await driver.close()


def main() -> int:
    args = parse_args()
    try:
        return asyncio.run(main_async(args))
    except KeyboardInterrupt:
        print("\n[interrupted]")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
