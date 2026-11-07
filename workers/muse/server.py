"""HTTP & SSE RPC Server for Muse Worker."""

from __future__ import annotations

import argparse
import asyncio
from contextlib import aclosing
import json
import logging
import os
import sys
from typing import Optional

from aiohttp import web

from .driver_base import MuseDriver
from .driver_mock import MockMuseDriver
from .protocol import (
    AccountInfo,
    ChatCompletedEvent,
    ChatDeltaEvent,
    ChatErrorEvent,
    ChatRequest,
)

logger = logging.getLogger("muse_worker.server")


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


DRIVER_KEY: web.AppKey[MuseDriver] = web.AppKey("driver", MuseDriver)
SECRET_KEY: web.AppKey[str] = web.AppKey("secret", str)


def create_app(
    driver: Optional[MuseDriver] = None,
    secret: Optional[str] = None,
) -> web.Application:
    app = web.Application()
    if driver is None:
        from .driver_engine import EngineMuseDriver
        driver = EngineMuseDriver()
    app[DRIVER_KEY] = driver
    app[SECRET_KEY] = secret or os.getenv("MUSE_WORKER_SECRET", "")

    async def handle_health(request: web.Request) -> web.Response:
        cur_driver = request.app[DRIVER_KEY]
        return web.json_response(
            {
                "status": "ok",
                "version": "1.0.0",
                "driver": cur_driver.__class__.__name__,
            }
        )

    async def handle_chat_completions(request: web.Request) -> web.StreamResponse:
        if not check_auth(request, request.app[SECRET_KEY]):
            return web.json_response(
                {"error": "unauthorized worker request", "code": "unauthorized"},
                status=401,
            )

        try:
            body = await request.json()
            chat_req = ChatRequest.from_dict(body)
        except Exception as e:
            return web.json_response(
                {"error": f"invalid json payload: {e}", "code": "invalid_request"},
                status=400,
            )

        cur_driver: MuseDriver = request.app[DRIVER_KEY]

        # 1. Non-streaming response
        if not chat_req.stream:
            text_buf = []
            reasoning_buf = []
            final_completed: Optional[ChatCompletedEvent] = None
            final_error: Optional[ChatErrorEvent] = None

            try:
                async with aclosing(cur_driver.chat_completion(chat_req)) as events:
                    async for event in events:
                        if isinstance(event, ChatDeltaEvent):
                            if event.text_delta:
                                text_buf.append(event.text_delta)
                            if event.reasoning_delta:
                                reasoning_buf.append(event.reasoning_delta)
                        elif isinstance(event, ChatCompletedEvent):
                            final_completed = event
                            break
                        elif isinstance(event, ChatErrorEvent):
                            final_error = event
                            break
            except asyncio.CancelledError:
                raise
            except Exception as e:
                return web.json_response(
                    {"error": str(e), "code": "internal_error"},
                    status=500,
                )

            if final_error:
                return web.json_response(
                    final_error.to_dict(),
                    status=final_error.status,
                )

            if final_completed is None:
                return web.json_response({"error": "driver ended without completed", "code": "incomplete_stream"}, status=502)

            res_text = "".join(text_buf)
            res_reasoning = "".join(reasoning_buf)
            usage = final_completed.usage if final_completed else {}
            cookies = final_completed.cookies if final_completed else None

            return web.json_response(
                {
                    "status": "ok",
                    "request_id": chat_req.request_id,
                    "model": chat_req.model,
                    "text": res_text or (final_completed.text if final_completed else ""),
                    "reasoning": res_reasoning or (final_completed.reasoning if final_completed else ""),
                    "finish_reason": final_completed.finish_reason if final_completed else "stop",
                    "usage": usage,
                    "cookies": cookies,
                }
            )

        # 2. Streaming SSE response
        response = web.StreamResponse(
            status=200,
            reason="OK",
            headers={
                "Content-Type": "text/event-stream; charset=utf-8",
                "Cache-Control": "no-cache, no-transform",
                "Connection": "keep-alive",
                "X-Accel-Buffering": "no",
            },
        )
        await response.prepare(request)

        try:
            terminal = False
            async with aclosing(cur_driver.chat_completion(chat_req)) as events:
                async for event in events:
                    await response.write(event.to_sse().encode("utf-8"))
                    if isinstance(event, (ChatCompletedEvent, ChatErrorEvent)):
                        terminal = True
                        break
            if not terminal:
                event = ChatErrorEvent("driver ended without completed", "incomplete_stream", 502)
                await response.write(event.to_sse().encode("utf-8"))
        except asyncio.CancelledError:
            logger.info("Stream cancelled by client for request %s", chat_req.request_id)
            raise
        except ConnectionResetError:
            logger.info("Stream client disconnected for request %s", chat_req.request_id)
        except Exception as e:
            err_event = ChatErrorEvent(error=str(e), code="stream_error", status=500)
            try:
                await response.write(err_event.to_sse().encode("utf-8"))
            except ConnectionResetError:
                logger.info("Stream client disconnected while reporting an error")

        return response

    async def handle_probe(request: web.Request) -> web.Response:
        if not check_auth(request, request.app[SECRET_KEY]):
            return web.json_response(
                {"error": "unauthorized worker request", "code": "unauthorized"},
                status=401,
            )

        try:
            body = await request.json()
            account = AccountInfo.from_dict(body)
        except Exception as e:
            return web.json_response(
                {"error": f"invalid json payload: {e}", "code": "invalid_request"},
                status=400,
            )

        cur_driver: MuseDriver = request.app[DRIVER_KEY]
        result = await cur_driver.probe_account(account)
        return web.json_response(result.to_dict())

    app.router.add_get("/health", handle_health)
    app.router.add_get("/v1/health", handle_health)
    app.router.add_post("/v1/chat/completions", handle_chat_completions)
    app.router.add_post("/v1/account/probe", handle_probe)

    async def on_cleanup(app_instance: web.Application) -> None:
        driver_to_close = app_instance.get(DRIVER_KEY)
        if driver_to_close:
            await driver_to_close.close()

    app.on_cleanup.append(on_cleanup)
    return app


def main() -> None:
    parser = argparse.ArgumentParser(description="DS2API Muse Worker")
    parser.add_argument("--host", default="127.0.0.1", help="Host to bind")
    parser.add_argument("--port", type=int, default=8765, help="Port to listen on")
    default_driver = os.getenv("MUSE_WORKER_DRIVER", "engine")
    parser.add_argument(
        "--driver",
        choices=["mock", "engine"],
        default=default_driver,
        help="Driver implementation to use (default: engine; mock is for offline tests)",
    )
    parser.add_argument(
        "--secret",
        default="",
        help="Optional shared authentication secret for requests",
    )
    args = parser.parse_args()

    logging.basicConfig(level=logging.INFO)
    if args.driver == "engine":
        from .driver_engine import EngineMuseDriver

        driver: MuseDriver = EngineMuseDriver()
    else:
        driver = MockMuseDriver()

    app = create_app(driver=driver, secret=args.secret)
    web.run_app(app, host=args.host, port=args.port, handler_cancellation=True)


if __name__ == "__main__":
    main()
