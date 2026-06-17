"""
OrchestrationService — ties A2A task lifecycle to adapter invocations.

The orchestrator sits between the A2A gateway / API layer and the
ProcessPool.  It is responsible for:

  * Registering and warming adapters on startup.
  * Routing incoming tasks to the best-matching adapter based on skill
    overlap scoring.
  * Managing the full task lifecycle (invoke / streaming / cancel).
  * Releasing pooled processes after each invocation.

The orchestrator is framework-agnostic: it only interacts with adapters
through the ``AgentWrapper`` ABC and the shared ``InvokeRequest`` /
``InvokeResponse`` / ``StreamEvent`` types.
"""

from __future__ import annotations

import asyncio
import logging
import time
import uuid
from dataclasses import dataclass, field
from typing import Any, AsyncIterator

from oap.adapters.errors import AdapterError, FrameworkNotFoundError
from oap.adapters.pool import PoolConfig, ProcessPool, ProcessState
from oap.adapters.types import (
    AgentCard,
    HealthStatus,
    InvokeRequest,
    InvokeResponse,
    Part,
    StreamEvent,
)
from oap.adapters.wrapper import ADAPTER_REGISTRY, AgentWrapper

logger = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# Data classes
# ---------------------------------------------------------------------------


@dataclass
class OrchestratorConfig:
    """Runtime configuration for the OrchestrationService.

    Attributes:
        pool_config: Configuration passed to the underlying ProcessPool.
        default_timeout: Default invocation timeout in seconds when the
            caller does not supply one.
    """

    pool_config: PoolConfig = field(default_factory=PoolConfig)
    default_timeout: float = 120.0


@dataclass
class AdapterInfo:
    """Public-facing adapter descriptor returned by ``supported_adapters``.

    Attributes:
        name: Registry key of the adapter.
        agent_card: Static discovery card for the adapter.
        healthy: ``True`` if the last health check succeeded.
    """

    name: str
    agent_card: AgentCard
    healthy: bool = True


# ---------------------------------------------------------------------------
# OrchestrationService
# ---------------------------------------------------------------------------


class OrchestrationService:
    """Routes A2A tasks to the best-matching adapter via a ProcessPool."""

    def __init__(self, config: OrchestratorConfig | None = None) -> None:
        self._config = config or OrchestratorConfig()
        self._pool = ProcessPool(self._config.pool_config)
        self._adapters: dict[str, AgentWrapper] = {}
        self._adapter_cards: dict[str, AgentCard] = {}
        self._task_to_adapter: dict[str, str] = {}
        self._start_time: float = 0.0
        self._started = False

    # -- Lifecycle ----------------------------------------------------------

    async def start(self) -> None:
        """Register all installed adapters and warm the process pool.

        Called once during application startup.
        """
        if self._started:
            return

        self._started = True
        self._start_time = time.monotonic()

        # Register every adapter that was imported via @register_adapter.
        for name, cls in ADAPTER_REGISTRY.items():
            instance = cls()
            await instance.start()
            self._adapters[name] = instance
            self._adapter_cards[name] = instance.agent_card
            logger.info("Registered adapter %r", name)

        # Warm the process pool.
        self._pool._config.adapters = list(self._adapters.keys())
        await self._pool.start()

    async def stop(self) -> None:
        """Drain the process pool and stop every adapter.

        Called once during application shutdown.
        """
        if not self._started:
            return

        await self._pool.stop()

        for name, adapter in self._adapters.items():
            try:
                await adapter.stop()
            except Exception:
                logger.exception("Error stopping adapter %r", name)

        self._started = False

    # -- Adapter matching ---------------------------------------------------

    def _score_adapter(
        self,
        adapter_name: str,
        required_skills: list[str],
        preferred_agent: str,
    ) -> float:
        """Return a skill-overlap score for *adapter_name*.

        Args:
            adapter_name: Registry key of the adapter to score.
            required_skills: Caller-supplied list of skill names.
            preferred_agent: Caller-supplied preferred adapter name.

        Returns:
            A score between 0.0 and 1.0 (higher is better).  A direct
            ``preferred_agent`` match always yields the maximum score.
        """
        if preferred_agent and adapter_name == preferred_agent:
            return 1.0

        card = self._adapter_cards.get(adapter_name)
        if card is None or not required_skills:
            return 0.0

        adapter_skills = {s.name for s in card.skills}
        if not adapter_skills:
            return 0.0

        overlap = adapter_skills.intersection(required_skills)
        return len(overlap) / len(required_skills)

    def _match_adapter(
        self,
        required_skills: list[str],
        preferred_agent: str,
    ) -> str:
        """Return the name of the best-matching registered adapter.

        Args:
            required_skills: Skill names the task requires.
            preferred_agent: Adapter name preferred by the caller.

        Returns:
            The registry key of the chosen adapter.

        Raises:
            FrameworkNotFoundError: If no adapter matches.
        """
        if not self._adapters:
            raise FrameworkNotFoundError("No adapters are registered")

        # If the caller names a preferred adapter and it exists, use it.
        if preferred_agent and preferred_agent in self._adapters:
            return preferred_agent

        # Otherwise, score by skill overlap.
        scored = [
            (self._score_adapter(name, required_skills, preferred_agent), name)
            for name in self._adapters
        ]
        scored.sort(key=lambda pair: pair[0], reverse=True)

        best_score, best_name = scored[0]
        if best_score > 0.0:
            return best_name

        # No skill overlap and no preferred agent — fall back to ozore
        # (the default hosted LLM agent), or the first registered adapter.
        return self._adapters.get("ozore", next(iter(self._adapters)))

    # -- Public API ---------------------------------------------------------

    async def handle_task(
        self,
        task_id: str,
        request: InvokeRequest,
    ) -> InvokeResponse:
        """Route and execute a single request/response task.

        Args:
            task_id: Platform task identifier.
            request: The invocation request.

        Returns:
            The terminal ``InvokeResponse``.
        """
        metadata = request.metadata or {}
        preferred_agent: str = metadata.get("preferred_agent", "")
        required_skills: list[str] = metadata.get("required_skills", [])

        adapter_name = self._match_adapter(required_skills, preferred_agent)
        self._task_to_adapter[task_id] = adapter_name

        request.task_id = task_id
        if not request.timeout_seconds:
            request.timeout_seconds = self._config.default_timeout

        pooled = await self._pool.acquire(adapter_name)
        pooled.active_tasks.add(task_id)

        try:
            response = await self._invoke_with_timeout(pooled, adapter_name, request)
            return response
        except AdapterError:
            raise
        except Exception as exc:
            return InvokeResponse(
                task_id=task_id,
                status="failed",
                error_message=str(exc),
            )
        finally:
            self._pool._processes.get(pooled.process_id)  # touch to keep ref
            await self._pool.release(pooled)
            self._task_to_adapter.pop(task_id, None)

    async def handle_streaming_task(
        self,
        task_id: str,
        request: InvokeRequest,
    ) -> AsyncIterator[StreamEvent]:
        """Route and execute a streaming task.

        Yields ``StreamEvent`` objects as they arrive from the adapter.
        """
        metadata = request.metadata or {}
        preferred_agent: str = metadata.get("preferred_agent", "")
        required_skills: list[str] = metadata.get("required_skills", [])

        adapter_name = self._match_adapter(required_skills, preferred_agent)
        self._task_to_adapter[task_id] = adapter_name

        request.task_id = task_id
        if not request.timeout_seconds:
            request.timeout_seconds = self._config.default_timeout

        pooled = await self._pool.acquire(adapter_name)
        pooled.active_tasks.add(task_id)

        try:
            async for event in self._stream_with_timeout(pooled, adapter_name, request):
                yield event
        finally:
            await self._pool.release(pooled)
            self._task_to_adapter.pop(task_id, None)

    async def cancel_task(self, task_id: str) -> bool:
        """Cancel an in-flight task.

        Returns:
            ``True`` if cancellation was delivered, ``False`` otherwise.
        """
        adapter_name = self._task_to_adapter.get(task_id)
        if adapter_name is None:
            return False

        cancelled = await self._pool.cancel(task_id)
        if cancelled:
            self._task_to_adapter.pop(task_id, None)
        return cancelled

    def supported_adapters(self) -> list[AdapterInfo]:
        """Return a list of all registered adapters with health status."""
        result: list[AdapterInfo] = []
        for name, card in self._adapter_cards.items():
            healthy = True
            for pooled in self._pool._processes.values():
                if pooled.adapter_name == name and not pooled.health_ok:
                    healthy = False
                    break
            result.append(AdapterInfo(name=name, agent_card=card, healthy=healthy))
        return result

    async def adapter_health(self, name: str) -> HealthStatus:
        """Return a health snapshot for a single adapter."""
        if name not in self._adapters:
            return HealthStatus(healthy=False, last_error=f"adapter '{name}' not found")
        return await self._pool.health(name)

    @property
    def pool(self) -> ProcessPool:
        """Expose the underlying process pool (for health endpoints)."""
        return self._pool

    # -- Internal helpers ---------------------------------------------------

    async def _invoke_with_timeout(
        self,
        pooled: Any,
        adapter_name: str,
        request: InvokeRequest,
    ) -> InvokeResponse:
        """Send INVOKE via the pool, enforcing the request timeout."""
        try:
            return await asyncio.wait_for(
                self._pool.invoke(adapter_name, request),
                timeout=request.timeout_seconds,
            )
        except asyncio.TimeoutError:
            # Attempt to cancel on timeout, then release.
            try:
                await self._pool.cancel(request.task_id)
            except Exception:
                logger.exception("Failed to cancel timed-out task %s", request.task_id)
            return InvokeResponse(
                task_id=request.task_id,
                status="failed",
                error_message=f"Invocation timed out after {request.timeout_seconds}s",
            )

    async def _stream_with_timeout(
        self,
        pooled: Any,
        adapter_name: str,
        request: InvokeRequest,
    ) -> AsyncIterator[StreamEvent]:
        """Yield stream events from the pool, enforcing the request timeout."""
        try:
            async with asyncio.timeout(request.timeout_seconds):
                async for event in self._pool.stream(adapter_name, request):
                    yield event
        except (asyncio.TimeoutError, TimeoutError):
            try:
                await self._pool.cancel(request.task_id)
            except Exception:
                logger.exception("Failed to cancel timed-out stream %s", request.task_id)
            yield StreamEvent(
                task_id=request.task_id,
                event_type="error",
                status="timeout",
                metadata={"error": f"Stream timed out after {request.timeout_seconds}s"},
            )