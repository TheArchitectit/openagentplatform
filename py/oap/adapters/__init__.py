"""
OAP Agent Adapters — framework-agnostic agent invocation layer.

Public exports for the adapter subsystem: shared types, the AgentWrapper ABC,
adapter registry, and exception taxonomy. Framework adapters (LangGraph,
CrewAI, AutoGen, etc.) will live in subpackages and use lazy imports.
"""

from oap.adapters.errors import (
    AdapterError,
    FrameworkNotFoundError,
    InvocationError,
    TimeoutError,
)
from oap.adapters.types import (
    AgentCard,
    AgentSkill,
    CostRecord,
    HealthStatus,
    InvokeRequest,
    InvokeResponse,
    Part,
    StreamEvent,
)
from oap.adapters.wrapper import ADAPTER_REGISTRY, AgentWrapper, register_adapter

__all__ = [
    # Types
    "AgentSkill",
    "AgentCard",
    "InvokeRequest",
    "InvokeResponse",
    "StreamEvent",
    "HealthStatus",
    "Part",
    "CostRecord",
    # ABC and registry
    "AgentWrapper",
    "ADAPTER_REGISTRY",
    "register_adapter",
    # Errors
    "AdapterError",
    "InvocationError",
    "TimeoutError",
    "FrameworkNotFoundError",
]
