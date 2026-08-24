"""
Process pool for adapter subprocess management.

Each adapter runs in its own subprocess for fault isolation. The pool keeps
warm processes pre-loaded, manages LRU eviction, and handles health checks
via length-prefixed JSON over stdin/stdout.

The protocol types and the spawn / evict / health-check internals live in
``pool_types`` and ``pool_lifecycle`` respectively.
"""

from __future__ import annotations

import asyncio
import time
import uuid
from collections.abc import AsyncIterator
from pathlib import Path

from oap.adapters.pool_lifecycle import PoolLifecycleMixin
from oap.adapters.pool_types import (
    _MSG_CANCEL,
    _MSG_HEALTH,
    _MSG_INVOKE,
    _MSG_STREAM_START,
    PoolConfig,
    PooledProcess,
    ProcessState,
    _read_frame,
    _write_frame,
)
from oap.adapters.types import (
    HealthStatus,
    InvokeRequest,
    InvokeResponse,
    StreamEvent,
)

__all__ = ["PoolConfig", "PooledProcess", "ProcessState", "ProcessPool"]


class ProcessPool(PoolLifecycleMixin):
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

        self._health_task = asyncio.create_task(self._health_check_loop(), name="oap-pool-health")

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

    async def invoke(self, adapter_name: str, request: InvokeRequest) -> InvokeResponse:
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

    # -- Streaming support (skeleton) ---------------------------------------

    async def stream(self, adapter_name: str, request: InvokeRequest) -> AsyncIterator[StreamEvent]:  # type: ignore[name-defined]
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
