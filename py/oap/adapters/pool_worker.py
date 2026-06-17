"""
Worker process entry point for the adapter pool.

Each worker loads a single adapter instance and communicates with the pool
over length-prefixed JSON frames on stdin/stdout. Supported commands:
    INVOKE, CANCEL, HEALTH, STREAM_START, STREAM_CANCEL

Usage:
    python pool_worker.py <adapter_name>
"""

from __future__ import annotations

import asyncio
import json
import struct
import sys
import time
from typing import Any

from oap.adapters.wrapper import ADAPTER_REGISTRY, AgentWrapper

_FRAME_HEADER_LEN = 4


# ---------------------------------------------------------------------------
# Frame I/O
# ---------------------------------------------------------------------------


async def _write_frame(payload: dict[str, Any]) -> None:
    """Write a length-prefixed JSON frame to stdout."""
    data = json.dumps(payload, default=str).encode("utf-8")
    header = struct.pack(">I", len(data))
    sys.stdout.buffer.write(header + data)
    sys.stdout.buffer.flush()


async def _read_frame() -> dict[str, Any] | None:
    """Read a length-prefixed JSON frame from stdin.  Returns None on EOF."""
    header = sys.stdin.buffer.read(_FRAME_HEADER_LEN)
    if not header or len(header) < _FRAME_HEADER_LEN:
        return None
    (length,) = struct.unpack(">I", header)
    body = sys.stdin.buffer.read(length)
    if not body:
        return None
    return json.loads(body.decode("utf-8"))


# ---------------------------------------------------------------------------
# Command handlers
# ---------------------------------------------------------------------------


async def _cmd_invoke(adapter: AgentWrapper, msg: dict[str, Any]) -> None:
    """Handle an INVOKE command."""
    from oap.adapters.types import InvokeRequest, InvokeResponse

    task_id = msg.get("task_id", "")
    request_json = msg.get("request_json", {})
    request = InvokeRequest(**request_json)
    response: InvokeResponse = await adapter.invoke(request)
    await _write_frame(response.model_dump())


async def _cmd_cancel(adapter: AgentWrapper, msg: dict[str, Any]) -> None:
    """Handle a CANCEL command."""
    task_id = msg.get("task_id", "")
    cancelled = await adapter.cancel(task_id)
    await _write_frame({"cancelled": cancelled})


async def _cmd_health(adapter: AgentWrapper, _msg: dict[str, Any]) -> None:
    """Handle a HEALTH command."""
    status = await adapter.health()
    await _write_frame({"healthy": status.healthy, "stats": status.model_dump()})


async def _cmd_stream_start(adapter: AgentWrapper, msg: dict[str, Any]) -> None:
    """Handle a STREAM_START command — yields each event as a frame."""
    from oap.adapters.types import InvokeRequest, StreamEvent

    task_id = msg.get("task_id", "")
    request_json = msg.get("request_json", {})
    request = InvokeRequest(**request_json)
    async for event in adapter.stream(request):
        await _write_frame(event.model_dump())


# ---------------------------------------------------------------------------
# Main loop
# ---------------------------------------------------------------------------


async def run(adapter: AgentWrapper) -> None:
    """Read commands from stdin and dispatch to the adapter."""
    await adapter.start()
    try:
        while True:
            msg = await _read_frame()
            if msg is None:
                break
            cmd = msg.get("cmd", "")
            try:
                if cmd == "INVOKE":
                    await _cmd_invoke(adapter, msg)
                elif cmd == "CANCEL":
                    await _cmd_cancel(adapter, msg)
                elif cmd == "HEALTH":
                    await _cmd_health(adapter, msg)
                elif cmd == "STREAM_START":
                    await _cmd_stream_start(adapter, msg)
                else:
                    await _write_frame({"error": f"unknown command: {cmd}"})
            except Exception as exc:
                await _write_frame({"error": str(exc)})
    finally:
        await adapter.stop()


def main() -> None:
    """Entry point: parse adapter name, instantiate, run loop."""
    if len(sys.argv) < 2:
        sys.stderr.write("Usage: pool_worker.py <adapter_name>\n")
        sys.exit(1)

    adapter_name = sys.argv[1]
    cls = ADAPTER_REGISTRY.get(adapter_name)
    if cls is None:
        sys.stderr.write(f"Unknown adapter: {adapter_name}\n")
        sys.exit(1)

    adapter = cls()
    asyncio.run(run(adapter))


if __name__ == "__main__":
    main()
