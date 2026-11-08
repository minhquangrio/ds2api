"""Shared HTTP plumbing for the Muse worker RPC server and media routes."""

from __future__ import annotations

import logging
from typing import Optional, Tuple

from aiohttp import web

from .driver_base import MuseDriver
from .job_store import JobRegistry

logger = logging.getLogger("muse_worker.http")

DRIVER_KEY: web.AppKey[MuseDriver] = web.AppKey("driver", MuseDriver)
SECRET_KEY: web.AppKey[str] = web.AppKey("secret", str)
STORE_KEY: web.AppKey[JobRegistry] = web.AppKey("job_store", JobRegistry)


def check_auth(request: web.Request, expected_secret: Optional[str]) -> bool:
    if not expected_secret:
        return True
    header_secret = request.headers.get("X-Worker-Secret")
    if header_secret == expected_secret:
        return True
    auth_header = request.headers.get("Authorization", "")
    if auth_header.startswith("Bearer "):
        token = auth_header[7:].strip()
        if token == expected_secret:
            return True
    return False


def unauthorized() -> web.Response:
    return web.json_response({"error": "unauthorized worker request", "code": "unauthorized"}, status=401)


def error_response(message: str, code: str, status: int) -> web.Response:
    return web.json_response({"error": message, "code": code, "status": status}, status=status)


async def read_json(request: web.Request) -> Tuple[Optional[dict], Optional[web.Response]]:
    """Return (payload, None) or (None, error response); never raises into the route."""
    try:
        return await request.json(), None
    except Exception as exc:
        return None, web.json_response(
            {"error": f"invalid json payload: {exc}", "code": "invalid_request"}, status=400,
        )


def driver_of(app: web.Application) -> MuseDriver:
    return app[DRIVER_KEY]


def store_of(app: web.Application) -> JobRegistry:
    return app[STORE_KEY]
