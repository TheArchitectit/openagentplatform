"""
Data models for the orchestration service.

Holds the runtime configuration and the public adapter descriptor used by
:class:`oap.adapters.orchestrator.OrchestrationService`.
"""

from __future__ import annotations

from dataclasses import dataclass, field

from oap.adapters.pool import PoolConfig
from oap.adapters.types import AgentCard


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
