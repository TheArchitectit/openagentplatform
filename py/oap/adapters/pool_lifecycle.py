"""
Lifecycle helpers for the adapter process pool.

Contains the subprocess spawn / evict / terminate / health-check machinery
used by :class:`oap.adapters.pool.ProcessPool`, factored out into a small
mixin so the pool's public API stays readable.
"""

from __future__ import annotations

import asyncio
import time
import uuid

from oap.adapters.pool_types import (
    _MSG_HEALTH,
    PooledProcess,
    ProcessState,
    _read_frame,
    _write_frame,
)


class PoolLifecycleMixin:
    """Spawn, evict, and health-check logic for pooled subprocesses.

    The mixin relies on instance attributes provided by ``ProcessPool``:
    ``_processes``, ``_lock``, and ``_config`` (plus ``_worker_path``).
    """

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
                if p.state == ProcessState.IDLE and (now - p.last_used) > self._config.idle_timeout
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
        except TimeoutError:
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
                    except (TimeoutError, BrokenPipeError, ConnectionResetError, OSError):
                        dead.append(pooled)

                for pooled in dead:
                    await self._terminate(pooled)
                    del self._processes[pooled.process_id]
                    if pooled.state != ProcessState.STOPPED:
                        # Replace with a fresh process.
                        await self._spawn(pooled.adapter_name)

            # Outside the lock: also evict long-idle processes.
            await self._evict_idle_timeout()
