"""
Klein Registrar Service - HTTP API

This FastAPI service provides the same API as Klein's Go register service,
allowing the frontend to manage the Python registrar.
"""

import asyncio
import json
import uuid
from datetime import datetime, timezone
from typing import Any

import uvicorn
from fastapi import FastAPI, Header, HTTPException, Query
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import StreamingResponse

from services.register_service import register_service

app = FastAPI(title="Klein Registrar Service", version="0.1.0")

# CORS for frontend
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _trace_id() -> str:
    return uuid.uuid4().hex


def _wrap_response(data: Any, trace_id: str = None) -> dict:
    """Wrap response in Klein API standard format"""
    return {
        "code": 0,
        "msg": "ok",
        "data": data,
        "trace_id": trace_id or _trace_id(),
    }


# === API Endpoints ===

@app.get("/admin/api/v1/register")
async def get_config(authorization: str = Header(None)):
    """Get current registration configuration"""
    cfg = register_service.get()
    return _wrap_response({"register": cfg})


@app.post("/admin/api/v1/register")
async def update_config(authorization: str = Header(None), body: dict = None):
    """Update registration configuration"""
    if body is None:
        body = {}

    # Handle mail configuration
    if body.get("mail") and isinstance(body["mail"], dict):
        mail_updates = body["mail"]
        # Normalize mail providers
        providers = []
        for p in mail_updates.get("providers") or []:
            if isinstance(p, dict) and p.get("type"):
                providers.append({
                    "type": p.get("type"),
                    "enable": p.get("enable", True),
                    "api_base": p.get("api_base", ""),
                    "api_key": p.get("api_key", ""),
                    "admin_password": p.get("admin_password", ""),
                    "domain": p.get("domain", []),
                    "default_domain": p.get("default_domain", ""),
                    "ddg_token": p.get("ddg_token", ""),
                    "cf_inbox_jwt": p.get("cf_inbox_jwt", ""),
                    "cf_api_base": p.get("cf_api_base", ""),
                    "cf_api_key": p.get("cf_api_key", ""),
                    "cf_auth_mode": p.get("cf_auth_mode", ""),
                    "cf_domain": p.get("cf_domain", []),
                    "expiry_time": p.get("expiry_time", 0),
                })
        mail_config = {
            "request_timeout": mail_updates.get("request_timeout", 30),
            "wait_timeout": mail_updates.get("wait_timeout", 30),
            "wait_interval": mail_updates.get("wait_interval", 2),
            "user_agent": mail_updates.get("user_agent", ""),
            "proxy": mail_updates.get("proxy", ""),
            "providers": providers,
        }
        body["mail"] = mail_config

    cfg = register_service.update(body)
    return _wrap_response({"register": cfg})


@app.post("/admin/api/v1/register/start")
async def start_registration(authorization: str = Header(None)):
    """Start registration process"""
    cfg = register_service.start()
    return _wrap_response({"register": cfg})


@app.post("/admin/api/v1/register/stop")
async def stop_registration(authorization: str = Header(None)):
    """Stop registration process"""
    cfg = register_service.stop()
    return _wrap_response({"register": cfg})


@app.post("/admin/api/v1/register/reset")
async def reset_stats(authorization: str = Header(None)):
    """Reset statistics"""
    cfg = register_service.reset()
    return _wrap_response({"register": cfg})


@app.get("/admin/api/v1/register/events")
async def events_stream(authorization: str = Header(None), token: str = Query(None)):
    """SSE stream for registration updates"""

    async def event_generator():
        # Send initial config
        cfg = register_service.get()
        yield f"data: {json.dumps(cfg, ensure_ascii=False)}\n\n"

        # Create a callback for updates
        updates_queue = asyncio.Queue()

        def sync_callback(config_dict):
            try:
                updates_queue.put_nowait(config_dict)
            except:
                pass

        register_service.subscribe(sync_callback)

        try:
            while True:
                try:
                    # Wait for update with timeout
                    update = await asyncio.wait_for(updates_queue.get(), timeout=30.0)
                    yield f"data: {json.dumps(update, ensure_ascii=False)}\n\n"
                except asyncio.TimeoutError:
                    # Send heartbeat
                    yield ": heartbeat\n\n"
        finally:
            register_service.unsubscribe(sync_callback)

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "Transfer-Encoding": "chunked",
        }
    )


@app.get("/ping")
async def ping():
    """Health check"""
    return {"pong": True, "service": "registrar", "time": _now()}


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8080, access_log=True)