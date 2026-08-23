"""
Adapter conformance test suite.

Validates that every registered adapter satisfies the AgentWrapper contract:
  - agent_card returns a valid AgentCard
  - invoke accepts InvokeRequest and returns InvokeResponse
  - stream yields StreamEvent objects ending with "done"
  - cancel returns bool
  - health returns HealthStatus
  - start/stop lifecycle hooks exist

Run: pytest tests/test_adapter_conformance.py -v
"""

from __future__ import annotations

import asyncio
import importlib
import inspect

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


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

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


# Load once at module level.
_REGISTERED = _load_adapters()
_NAMES = [name for name, _ in _REGISTERED]


def _make_request() -> InvokeRequest:
    return InvokeRequest(
        task_id="conformance-test-1",
        messages=[Part(type="text", text="Hello, agent.")],
        metadata={},
        timeout_seconds=30.0,
    )


def _get_class(name: str) -> type[AgentWrapper]:
    return next(c for n, c in _REGISTERED if n == name)


def _get_instance(name: str) -> AgentWrapper:
    cls = _get_class(name)
    try:
        return cls()
    except Exception as e:
        if "not installed" in str(e).lower() or "not found" in str(e).lower():
            pytest.skip(f"Adapter '{name}' SDK not installed: {e}")
        raise


def _skip_sdk(e: Exception, name: str):
    msg = str(e).lower()
    if "not found" in msg or "not installed" in msg or "api" in msg:
        pytest.skip(f"Adapter '{name}' SDK unavailable: {e}")
    raise


# ---------------------------------------------------------------------------
# Conformance: Registration
# ---------------------------------------------------------------------------


class TestAdapterRegistration:
    EXPECTED = {"langgraph", "crewai", "autogen", "semantic_kernel", "openai_agents", "anthropic"}

    def test_all_adapters_registered(self):
        missing = self.EXPECTED - set(_NAMES)
        assert not missing, f"Missing adapters: {missing}"

    def test_registry_entries_are_subclasses(self):
        for name, cls in _REGISTERED:
            assert issubclass(cls, AgentWrapper), f"'{name}' not a subclass of AgentWrapper"

    def test_expected_count(self):
        assert len(_REGISTERED) >= 6, f"Expected >=6 adapters, got {len(_REGISTERED)}"


# ---------------------------------------------------------------------------
# Conformance: AgentCard
# ---------------------------------------------------------------------------


class TestAgentCardConformance:
    @pytest.fixture(params=_NAMES)
    def adapter_name(self, request):
        return request.param

    def _get_card(self, name: str) -> AgentCard:
        return _get_instance(name).agent_card

    def test_card_is_agent_card(self, adapter_name):
        assert isinstance(self._get_card(adapter_name), AgentCard)

    def test_card_has_name(self, adapter_name):
        assert self._get_card(adapter_name).name

    def test_card_has_version(self, adapter_name):
        assert self._get_card(adapter_name).version

    def test_card_has_skills(self, adapter_name):
        assert len(self._get_card(adapter_name).skills) > 0

    def test_skills_have_names(self, adapter_name):
        for skill in self._get_card(adapter_name).skills:
            assert isinstance(skill, AgentSkill)
            assert skill.name


# ---------------------------------------------------------------------------
# Conformance: Method Signatures
# ---------------------------------------------------------------------------


class TestMethodSignatures:
    @pytest.fixture(params=_NAMES)
    def adapter_name(self, request):
        return request.param

    def test_has_invoke(self, adapter_name):
        cls = _get_class(adapter_name)
        assert hasattr(cls, "invoke")
        assert inspect.iscoroutinefunction(cls.invoke)

    def test_has_stream(self, adapter_name):
        assert hasattr(_get_class(adapter_name), "stream")

    def test_has_cancel(self, adapter_name):
        cls = _get_class(adapter_name)
        assert hasattr(cls, "cancel")
        assert inspect.iscoroutinefunction(cls.cancel)

    def test_has_health(self, adapter_name):
        cls = _get_class(adapter_name)
        assert hasattr(cls, "health")
        assert inspect.iscoroutinefunction(cls.health)

    def test_has_start(self, adapter_name):
        assert hasattr(_get_class(adapter_name), "start")

    def test_has_stop(self, adapter_name):
        assert hasattr(_get_class(adapter_name), "stop")


# ---------------------------------------------------------------------------
# Conformance: Invoke (requires SDK — skipped if unavailable)
# ---------------------------------------------------------------------------


class TestInvokeConformance:
    @pytest.fixture(params=_NAMES)
    def adapter_name(self, request):
        return request.param

    def test_invoke_returns_response(self, adapter_name):
        instance = _get_instance(adapter_name)
        req = _make_request()
        try:
            resp = asyncio.run(instance.invoke(req))
        except Exception as e:
            _skip_sdk(e, adapter_name)
        assert isinstance(resp, InvokeResponse)
        assert resp.task_id == req.task_id
        assert resp.status in ("completed", "failed", "input_required", "canceled")

    def test_invoke_has_messages(self, adapter_name):
        instance = _get_instance(adapter_name)
        req = _make_request()
        try:
            resp = asyncio.run(instance.invoke(req))
        except Exception as e:
            _skip_sdk(e, adapter_name)
        if resp.status == "completed":
            assert len(resp.messages) > 0


# ---------------------------------------------------------------------------
# Conformance: Stream (requires SDK — skipped if unavailable)
# ---------------------------------------------------------------------------


class TestStreamConformance:
    @pytest.fixture(params=_NAMES)
    def adapter_name(self, request):
        return request.param

    def _collect_events(self, adapter_name: str) -> list[StreamEvent]:
        instance = _get_instance(adapter_name)
        req = _make_request()

        async def _run():
            events = []
            try:
                async for event in instance.stream(req):
                    events.append(event)
            except Exception as e:
                _skip_sdk(e, adapter_name)
            return events

        return asyncio.run(_run())

    def test_stream_yields_events(self, adapter_name):
        events = self._collect_events(adapter_name)
        assert len(events) > 0

    def test_stream_ends_with_done(self, adapter_name):
        events = self._collect_events(adapter_name)
        assert events[-1].event_type == "done"

    def test_stream_events_are_stream_event(self, adapter_name):
        events = self._collect_events(adapter_name)
        for event in events:
            assert isinstance(event, StreamEvent)
            assert event.event_type in ("delta", "status", "error", "done")


# ---------------------------------------------------------------------------
# Conformance: Health
# ---------------------------------------------------------------------------


class TestHealthConformance:
    @pytest.fixture(params=_NAMES)
    def adapter_name(self, request):
        return request.param

    def test_health_returns_status(self, adapter_name):
        instance = _get_instance(adapter_name)
        try:
            status = asyncio.run(instance.health())
        except Exception as e:
            _skip_sdk(e, adapter_name)
        assert isinstance(status, HealthStatus)
        assert isinstance(status.healthy, bool)


# ---------------------------------------------------------------------------
# Conformance: Cancel
# ---------------------------------------------------------------------------


class TestCancelConformance:
    @pytest.fixture(params=_NAMES)
    def adapter_name(self, request):
        return request.param

    def test_cancel_returns_bool(self, adapter_name):
        instance = _get_instance(adapter_name)
        try:
            result = asyncio.run(instance.cancel("nonexistent-task-id"))
        except Exception as e:
            _skip_sdk(e, adapter_name)
        assert isinstance(result, bool)


# ---------------------------------------------------------------------------
# Conformance: Lifecycle
# ---------------------------------------------------------------------------


class TestLifecycleConformance:
    @pytest.fixture(params=_NAMES)
    def adapter_name(self, request):
        return request.param

    def test_start_stop_no_error(self, adapter_name):
        instance = _get_instance(adapter_name)
        try:
            asyncio.run(instance.start())
            asyncio.run(instance.stop())
        except Exception as e:
            _skip_sdk(e, adapter_name)
