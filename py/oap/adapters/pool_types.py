"""
Shared protocol types and frame I/O for the adapter process pool.

The process pool (in :mod:`oap.adapters.pool`) communicates with worker
subprocesses via length-prefixed JSON frames over stdin/stdout.  This module
holds the message constants, the frame read/write primitives, and the
dataclasses that describe a single pooled subprocess.
"""

from __future__ import annotations

import asyncio
import enum
import json
import struct
import time
from dataclasses import dataclass, field
from typing import Any

# ---------------------------------------------------------------------------
# Protocol constants
# ---------------------------------------------------------------------------

_MSG_INVOKE = "INVOKE"
_MSG_CANCEL = "CANCEL"
_MSG_HEALTH = "HEALTH"
_MSG_STREAM_START = "STREAM_START"
_MSG_STREAM_CANCEL = "STREAM_CANCEL"

_FRAME_HEADER_LEN = 4  # length-prefix is 4 bytes (unsigned int, big-endian)


# ---------------------------------------------------------------------------
# Frame I/O — length-prefixed JSON over stdin/stdout
# ---------------------------------------------------------------------------


async def _write_frame(stream: asyncio.StreamWriter, payload: dict[str, Any]) -> None:
    """Write a single length-prefixed JSON frame to *stream*."""
    data = json.dumps(payload, default=str).encode("utf-8")
    header = struct.pack(">I", len(data))
    stream.write(header + data)
    await stream.drain()


async def _read_frame(stream: asyncio.StreamReader) -> dict[str, Any] | None:
    """Read a single length-prefixed JSON frame from *stream*.

    Returns ``None`` on EOF.
    """
    header = await stream.readexactly(_FRAME_HEADER_LEN)
    if not header:
        return None
    (length,) = struct.unpack(">I", header)
    body = await stream.readexactly(length)
    return json.loads(body.decode("utf-8"))


# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------


@dataclass
class PoolConfig:
    """Configuration for the process pool.

    Attributes:
        max_processes: Hard upper bound on concurrent subprocesses.
        idle_timeout: Seconds an idle process may live before eviction.
        health_check_interval: Seconds between health-check pings.
        warm_adapter_count: Pre-spawned idle processes per adapter name.
    """

    max_processes: int = 16
    idle_timeout: float = 300.0
    health_check_interval: float = 15.0
    warm_adapter_count: int = 3
    adapters: list[str] = field(default_factory=list)


# ---------------------------------------------------------------------------
# Process state
# ---------------------------------------------------------------------------


class ProcessState(str, enum.Enum):
    """Lifecycle states for a pooled subprocess."""

    STARTING = "starting"
    IDLE = "idle"
    BUSY = "busy"
    DRAINING = "draining"
    STOPPED = "stopped"


# ---------------------------------------------------------------------------
# PooledProcess — a single subprocess managed by the pool
# ---------------------------------------------------------------------------


@dataclass
class PooledProcess:
    """A single subprocess in the pool."""

    process_id: str
    adapter_name: str
    process: asyncio.subprocess.Process
    state: ProcessState = ProcessState.STARTING
    last_used: float = field(default_factory=time.monotonic)
    active_tasks: set[str] = field(default_factory=set)
    health_ok: bool = True
