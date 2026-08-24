"""Shared fixtures and helpers for the adapter conformance tests.

Every adapter module is imported once here so the registry is populated
before any test runs. Test files pull the helpers and the parametrized
``adapter_name`` fixture from this module.
"""

from __future__ import annotations

import importlib

import pytest

from oap.adapters.types import (
    AgentCard,
    AgentSkill,
    HealthStatus,
    InvokeRequest,
    InvokeResponse,
    Part,
    StreamEvent,
)
from oap.adapters.wrapper import ADAPTER_REGISTRY, AgentWrapper

__all__ = [
    "AgentCard",
    "AgentSkill",
    "AgentWrapper",
    "HealthStatus",
    "InvokeResponse",
    "StreamEvent",
]

ADAPTER_MODULES = [
    "oap.adapters.langgraph_adapter",
    "oap.adapters.crewai_adapter",
    "oap.adapters.autogen_adapter",
    "oap.adapters.semantic_kernel_adapter",
    "oap.adapters.openai_adapter",
    "oap.adapters.anthropic_adapter",
]


def _load_adapters() -> list[tuple[str, type[AgentWrapper]]]:
    """Import all adapter modules and return the populated registry."""
    for mod_name in ADAPTER_MODULES:
        try:
            importlib.import_module(mod_name)
        except Exception:
            pass
    return list(ADAPTER_REGISTRY.items())


# Load once at import time.
REGISTERED = _load_adapters()
NAMES = [name for name, _ in REGISTERED]


@pytest.fixture(params=NAMES)
def adapter_name(request):
    """Run a test once per registered adapter."""
    return request.param


def get_class(name: str) -> type[AgentWrapper]:
    return next(c for n, c in REGISTERED if n == name)


def get_instance(name: str) -> AgentWrapper:
    cls = get_class(name)
    try:
        return cls()
    except Exception as e:
        if "not installed" in str(e).lower() or "not found" in str(e).lower():
            pytest.skip(f"Adapter '{name}' SDK not installed: {e}")
        raise


def skip_sdk(e: Exception, name: str):
    msg = str(e).lower()
    if "not found" in msg or "not installed" in msg or "api" in msg:
        pytest.skip(f"Adapter '{name}' SDK unavailable: {e}")
    raise


def make_request() -> InvokeRequest:
    return InvokeRequest(
        task_id="conformance-test-1",
        messages=[Part(type="text", text="Hello, agent.")],
        metadata={},
        timeout_seconds=30.0,
    )
