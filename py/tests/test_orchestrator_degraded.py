"""Orchestrator.start() must degrade, not crash, when an adapter is missing.

Spec §11: the FastAPI app imports all adapter modules so @register_adapter
fires, and `ADAPTER_REGISTRY` is populated "without requiring the framework
packages to be installed." A missing SDK surfaces as FrameworkNotFoundError
from the adapter's __init__; start() must skip that adapter and keep the
service up (previously a single FrameworkNotFoundError aborted startup).
"""
from __future__ import annotations

import asyncio
import logging

import pytest

from oap.adapters.errors import FrameworkNotFoundError
from oap.adapters.orchestrator import OrchestrationService
from oap.adapters import wrapper as wrapper_mod


class _FakeUnavailable:
    """Constructs to raise FrameworkNotFoundError (SDK not installed)."""

    def __init__(self) -> None:
        raise FrameworkNotFoundError("fake framework is not installed", adapter_name="fake")


class _FakeAvailable:
    """A no-op adapter that registers cleanly."""

    agent_card = None  # orchestrator stores it; content is irrelevant here

    async def start(self) -> None:
        return None

    async def stop(self) -> None:
        return None


def test_start_skips_missing_framework(monkeypatch, caplog):
    monkeypatch.setitem(wrapper_mod.ADAPTER_REGISTRY, "fake", _FakeUnavailable)
    monkeypatch.setitem(wrapper_mod.ADAPTER_REGISTRY, "ok", _FakeAvailable)

    caplog.set_level(logging.WARNING)

    async def lifecycle():
        svc = OrchestrationService()
        # Must not raise even though one registered adapter cannot be
        # constructed; stop on the same loop start() warmed the pool on.
        await svc.start()
        try:
            return svc._adapters
        finally:
            await svc.stop()

    adapters = asyncio.run(lifecycle())
    assert "fake" not in adapters, "missing framework adapter should not register"
    assert "ok" in adapters, "available adapter must still register"
    assert any("fake" in r.getMessage().lower() and "unavailable" in r.getMessage().lower()
               for r in caplog.records if r.levelno >= logging.WARNING)


def test_start_survives_all_adapters_missing():
    # Replace the whole registry to simulate an environment with zero SDKs.
    saved = dict(wrapper_mod.ADAPTER_REGISTRY)
    wrapper_mod.ADAPTER_REGISTRY.clear()
    wrapper_mod.ADAPTER_REGISTRY["fake"] = _FakeUnavailable
    try:
        svc = OrchestrationService()
        asyncio.run(svc.start())
        assert svc._adapters == {}
    finally:
        wrapper_mod.ADAPTER_REGISTRY.clear()
        wrapper_mod.ADAPTER_REGISTRY.update(saved)


if __name__ == "__main__":
    import sys
    sys.exit(pytest.main([__file__, "-v"]))
