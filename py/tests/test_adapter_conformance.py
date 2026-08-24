"""
Adapter conformance test suite — static contract checks.

Validates that every registered adapter exposes the required AgentCard
shape and the expected method signatures, without invoking any SDK.
SDK-requiring runtime conformance (invoke/stream/health/cancel/lifecycle)
lives in test_adapter_runtime.py.

Run: pytest tests/test_adapter_conformance.py -v
"""

from __future__ import annotations

import inspect

from .conftest import (
    NAMES,
    REGISTERED,
    AgentCard,
    AgentSkill,
    AgentWrapper,
    get_class,
    get_instance,
)

# ---------------------------------------------------------------------------
# Conformance: Registration
# ---------------------------------------------------------------------------


class TestAdapterRegistration:
    EXPECTED = {"langgraph", "crewai", "autogen", "semantic_kernel", "openai_agents", "anthropic"}

    def test_all_adapters_registered(self):
        missing = self.EXPECTED - set(NAMES)
        assert not missing, f"Missing adapters: {missing}"

    def test_registry_entries_are_subclasses(self):
        for name, cls in REGISTERED:
            assert issubclass(cls, AgentWrapper), f"'{name}' not a subclass of AgentWrapper"

    def test_expected_count(self):
        assert len(REGISTERED) >= 6, f"Expected >=6 adapters, got {len(REGISTERED)}"


# ---------------------------------------------------------------------------
# Conformance: AgentCard (uses the conftest adapter_name fixture)
# ---------------------------------------------------------------------------


class TestAgentCardConformance:
    def _get_card(self, name: str) -> AgentCard:
        return get_instance(name).agent_card

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
# Conformance: Method Signatures (uses the conftest adapter_name fixture)
# ---------------------------------------------------------------------------


class TestMethodSignatures:
    def test_has_invoke(self, adapter_name):
        cls = get_class(adapter_name)
        assert hasattr(cls, "invoke")
        assert inspect.iscoroutinefunction(cls.invoke)

    def test_has_stream(self, adapter_name):
        assert hasattr(get_class(adapter_name), "stream")

    def test_has_cancel(self, adapter_name):
        cls = get_class(adapter_name)
        assert hasattr(cls, "cancel")
        assert inspect.iscoroutinefunction(cls.cancel)

    def test_has_health(self, adapter_name):
        cls = get_class(adapter_name)
        assert hasattr(cls, "health")
        assert inspect.iscoroutinefunction(cls.health)

    def test_has_start(self, adapter_name):
        assert hasattr(get_class(adapter_name), "start")

    def test_has_stop(self, adapter_name):
        assert hasattr(get_class(adapter_name), "stop")
