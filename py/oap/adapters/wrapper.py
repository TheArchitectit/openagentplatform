"""
AgentWrapper ABC and adapter registry.

Every framework adapter (LangGraph, CrewAI, AutoGen, Semantic Kernel,
OpenAI Agents SDK, Anthropic Claude) inherits from `AgentWrapper` and is
registered via the `@register_adapter` decorator. The rest of the platform
interacts with adapters exclusively through the `AgentWrapper` interface.
"""

from __future__ import annotations

from abc import ABC, abstractmethod
from typing import AsyncIterator

from oap.adapters.types import (
    AgentCard,
    HealthStatus,
    InvokeRequest,
    InvokeResponse,
    StreamEvent,
)

# ---------------------------------------------------------------------------
# Registry
# ---------------------------------------------------------------------------

ADAPTER_REGISTRY: dict[str, type[AgentWrapper]] = {}  # populated by @register_adapter


def register_adapter(name: str) -> callable:  # noqa: UP006
    """Class decorator that registers an AgentWrapper subclass in ADAPTER_REGISTRY.

    Usage:
        @register_adapter("langgraph")
        class LangGraphAdapter(AgentWrapper):
            ...

    Args:
        name: The registry key under which to store the adapter class.

    Returns:
        A decorator that registers the class and returns it unchanged.
    """

    def _decorator(cls: type[AgentWrapper]) -> type[AgentWrapper]:
        ADAPTER_REGISTRY[name] = cls
        return cls

    return _decorator


# ---------------------------------------------------------------------------
# AgentWrapper ABC
# ---------------------------------------------------------------------------


class AgentWrapper(ABC):
    """Abstract base class for all framework adapters.

    Concrete adapters MUST implement all five abstract members:
        - `agent_card` (property)
        - `invoke`
        - `stream`
        - `cancel`
        - `health`

    The optional lifecycle hooks `start` and `stop` have default no-op
    implementations; adapters that need explicit initialisation or
    teardown should override them.
    """

    # -- Discovery ---------------------------------------------------------

    @property
    @abstractmethod
    def agent_card(self) -> AgentCard:
        """Return the static AgentCard for this adapter.

        The card must be fully populated and must not require I/O to build.
        """
        ...

    # -- Invocation --------------------------------------------------------

    @abstractmethod
    async def invoke(self, req: InvokeRequest) -> InvokeResponse:
        """Execute a single request/response interaction.

        Args:
            req: The invocation request containing messages, metadata,
                and timeout.

        Returns:
            The terminal InvokeResponse with status, messages, artifacts,
            cost, and timing information.
        """
        ...

    @abstractmethod
    async def stream(self, req: InvokeRequest) -> AsyncIterator[StreamEvent]:
        """Execute a streaming interaction, yielding events as produced.

        Args:
            req: The invocation request.

        Yields:
            StreamEvent objects of type "delta", "status", "error", or
            "done". The stream MUST eventually yield at least one event
            with event_type "done".
        """
        ...

    @abstractmethod
    async def cancel(self, task_id: str) -> bool:
        """Cancel an in-flight task.

        Args:
            task_id: The identifier of the task to cancel.

        Returns:
            True if a cancellation signal was successfully delivered,
            False if the task was not found or already terminal.
        """
        ...

    @abstractmethod
    async def health(self) -> HealthStatus:
        """Return a snapshot of the adapter's health and resource usage."""
        ...

    # -- Lifecycle hooks (optional overrides) -------------------------------

    async def start(self) -> None:
        """Lifecycle hook invoked once when the adapter is activated.

        Default implementation is a no-op. Override to perform
        initialisation (e.g., building a compiled graph, opening
        connections).
        """
        return None

    async def stop(self) -> None:
        """Lifecycle hook invoked once when the adapter is deactivated.

        Default implementation is a no-op. Override to release resources
        (e.g., closing connections, draining background tasks).
        """
        return None
