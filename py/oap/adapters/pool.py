"""
Process pool for adapter subprocess management.

Each adapter runs in its own subprocess for fault isolation. The pool keeps
warm processes pre-loaded, manages LRU eviction, and handles health checks
via length-prefixed JSON over stdin/stdout.
"""

from __future__ import annotations

import asyncio
import enum
import json
import os
import struct
import time
import uuid
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from oap.adapters.types import (
    HealthStatus,
    InvokeRequest,
    InvokeResponse,
    StreamEvent,
)

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
# ProcessPool
# ---------------------------------------------------------------------------


class ProcessPool:
    """Manages warm subprocesses for adapter isolation.

    On ``start()`` the pool spawns ``warm_adapter_count`` processes for each
    adapter listed in ``PoolConfig.adapters``. Idle processes that exceed
    ``idle_timeout`` are evicted (LRU). A background health-check loop pings
    every ``health_check_interval`` seconds and replaces unresponsive
    processes.
    """

    def __init__(self, config: PoolConfig | None = None) -> None:
        self._config = config or PoolConfig()
        self._processes: dict[str, PooledProcess] = {}
        self._lock = asyncio.Lock()
        self._health_task: asyncio.Task[None] | None = None
        _worker = Path(__file__).parent / "pool_worker.py"
        self._worker_path = str(_worker)

    # -- Public properties --------------------------------------------------

    @property
    def config(self) -> PoolConfig:
        """Return the active pool configuration."""
        return self._config

    @property
    def total_processes(self) -> int:
        """Return the current number of live processes."""
        return len(self._processes)

    # -- Lifecycle ----------------------------------------------------------

    async def start(self) -> None:
        """Spawn warm subprocesses and start the health-check loop."""
        for adapter_name in self._config.adapters:
            for _ in range(self._config.warm_adapter_count):
                await self._spawn(adapter_name)

        self._health_task = asyncio.create_task(
            self._health_check_loop(), name="oap-pool-health"
        )

    async def stop(self) -> None:
        """Drain all processes and cancel background tasks."""
        if self._health_task is not None:
            self._health_task.cancel()
            try:
                await self._health_task
            except asyncio.CancelledError:
                pass
            self._health_task = None

        async with self._lock:
            for pooled in list(self._processes.values()):
                await self._terminate(pooled)
            self._processes.clear()

    # -- Pool operations ----------------------------------------------------

    async def acquire(self, adapter_name: str) -> PooledProcess:
        """Return an idle process for *adapter_name*, spawning or evicting
        as needed.  The returned process is marked BUSY.
        """
        async with self._lock:
            # 1. Find an idle, healthy process for this adapter.
            for pooled in self._processes.values():
                if (
                    pooled.adapter_name == adapter_name
                    and pooled.state == ProcessState.IDLE
                    and pooled.health_ok
                ):
                    pooled.state = ProcessState.BUSY
                    pooled.last_used = time.monotonic()
                    return pooled

            # 2. Try to evict an LRU idle process (from a different adapter
            #    or the same one) to make room.
            if len(self._processes) >= self._config.max_processes:
                evicted = await self._evict_lru()
                if evicted is None:
                    raise RuntimeError(
                        "No idle process available and pool is at capacity "
                        "with no evictable processes"
                    )

            # 3. Spawn a new process.
            return await self._spawn(adapter_name)

    async def release(self, pooled: PooledProcess) -> None:
        """Mark a previously-acquired process as IDLE."""
        async with self._lock:
            if pooled.state == ProcessState.BUSY:
                pooled.state = ProcessState.IDLE
            pooled.last_used = time.monotonic()
            pooled.active_tasks.clear()

    async def invoke(
        self, adapter_name: str, request: InvokeRequest
    ) -> InvokeResponse:
        """Send an INVOKE command to a pooled subprocess and await the
        response.
        """
        pooled = await self.acquire(adapter_name)
        task_id = request.task_id or str(uuid.uuid4())
        pooled.active_tasks.add(task_id)

        try:
            assert pooled.process.stdin is not None
            await _write_frame(
                pooled.process.stdin,
                {
                    "cmd": _MSG_INVOKE,
                    "task_id": task_id,
                    "adapter_name": adapter_name,
                    "request_json": request.model_dump(),
                },
            )
            assert pooled.process.stdout is not None
            frame = await _read_frame(pooled.process.stdout)
            if frame is None:
                pooled.health_ok = False
                raise RuntimeError(f"Process {pooled.process_id} died unexpectedly")
            return InvokeResponse(**frame)
        finally:
            await self.release(pooled)

    async def cancel(self, task_id: str) -> bool:
        """Send CANCEL to the process that owns *task_id*."""
        async with self._lock:
            for pooled in self._processes.values():
                if task_id in pooled.active_tasks:
                    assert pooled.process.stdin is not None
                    await _write_frame(
                        pooled.process.stdin,
                        {"cmd": _MSG_CANCEL, "task_id": task_id},
                    )
                    assert pooled.process.stdout is not None
                    frame = await _read_frame(pooled.process.stdout)
                    return bool(frame and frame.get("cancelled", False))
        return False

    async def health(self, adapter_name: str) -> HealthStatus:
        """Send HEALTH to a pooled subprocess for *adapter_name*."""
        async with self._lock:
            for pooled in self._processes.values():
                if pooled.adapter_name == adapter_name and pooled.state != ProcessState.STOPPED:
                    assert pooled.process.stdin is not None
                    await _write_frame(pooled.process.stdin, {"cmd": _MSG_HEALTH})
                    assert pooled.process.stdout is not None
                    frame = await _read_frame(pooled.process.stdout)
                    if frame is None:
                        pooled.health_ok = False
                        return HealthStatus(healthy=False, last_error="subprocess died")
                    return HealthStatus(**frame.get("stats", {}))
        return HealthStatus(healthy=False, last_error="no process available")

    # -- Internal helpers ---------------------------------------------------

    async def _spawn(self, adapter_name: str) -> PooledProcess:
        """Spawn a new subprocess for *adapter_name* and add it to the pool."""
        proc = await asyncio.create_subprocess_exec(
            "python",
            self._worker_path,
            adapter_name,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
        )
        pooled = PooledProcess(
            process_id=str(uuid.uuid4()),
            adapter_name=adapter_name,
            process=proc,
            state=ProcessState.IDLE,
        )
        self._processes[pooled.process_id] = pooled
        return pooled

    async def _evict_lru(self) -> PooledProcess | None:
        """Evict the least-recently-used IDLE process.  Returns the evicted
        pooled object, or ``None`` if nothing was evictable.
        """
        lru: PooledProcess | None = None
        for pooled in self._processes.values():
            if pooled.state == ProcessState.IDLE:
                if lru is None or pooled.last_used < lru.last_used:
                    lru = pooled
        if lru is not None:
            await self._terminate(lru)
            del self._processes[lru.process_id]
        return lru

    async def _evict_idle_timeout(self) -> None:
        """Remove processes that have been idle longer than ``idle_timeout``."""
        now = time.monotonic()
        async with self._lock:
            stale = [
                p
                for p in self._processes.values()
                if p.state == ProcessState.IDLE
                and (now - p.last_used) > self._config.idle_timeout
            ]
            for pooled in stale:
                await self._terminate(pooled)
                del self._processes[pooled.process_id]

    async def _terminate(self, pooled: PooledProcess) -> None:
        """Terminate a pooled subprocess."""
        pooled.state = ProcessState.STOPPED
        try:
            pooled.process.terminate()
        except ProcessLookupError:
            pass
        try:
            await asyncio.wait_for(pooled.process.wait(), timeout=5.0)
        except asyncio.TimeoutError:
            pooled.process.kill()
            await pooled.process.wait()

    async def _health_check_loop(self) -> None:
        """Periodically ping every process and restart unresponsive ones."""
        while True:
            try:
                await asyncio.sleep(self._config.health_check_interval)
            except asyncio.CancelledError:
                return

            async with self._lock:
                dead: list[PooledProcess] = []
                for pooled in list(self._processes.values()):
                    if pooled.state == ProcessState.STOPPED:
                        continue
                    if pooled.process.returncode is not None:
                        dead.append(pooled)
                        continue
                    try:
                        assert pooled.process.stdin is not None
                        await _write_frame(pooled.process.stdin, {"cmd": _MSG_HEALTH})
                        assert pooled.process.stdout is not None
                        frame = await asyncio.wait_for(
                            _read_frame(pooled.process.stdout), timeout=5.0
                        )
                        if frame is None:
                            dead.append(pooled)
                        else:
                            pooled.health_ok = frame.get("healthy", False)
                    except (asyncio.TimeoutError, BrokenPipeError, ConnectionResetError, OSError):
                        dead.append(pooled)

                for pooled in dead:
                    await self._terminate(pooled)
                    del self._processes[pooled.process_id]
                    if pooled.state != ProcessState.STOPPED:
                        # Replace with a fresh process.
                        await self._spawn(pooled.adapter_name)

            # Outside the lock: also evict long-idle processes.
            await self._evict_idle_timeout()

    # -- Streaming support (skeleton) ---------------------------------------

    async def stream(
        self, adapter_name: str, request: InvokeRequest
    ) -> AsyncIterator[StreamEvent]:  # type: ignore[name-defined]
        """Send STREAM_START and yield events from the subprocess.

        Yields:
            StreamEvent objects until a ``done`` event is received.
        """
        pooled = await self.acquire(adapter_name)
        task_id = request.task_id or str(uuid.uuid4())
        pooled.active_tasks.add(task_id)

        try:
            assert pooled.process.stdin is not None
            await _write_frame(
                pooled.process.stdin,
                {
                    "cmd": _MSG_STREAM_START,
                    "task_id": task_id,
                    "adapter_name": adapter_name,
                    "request_json": request.model_dump(),
                },
            )
            assert pooled.process.stdout is not None
            while True:
                frame = await _read_frame(pooled.process.stdout)
                if frame is None:
                    break
                event = StreamEvent(**frame)
                yield event
                if event.event_type == "done":
                    break
        finally:
            await self.release(pooled)
