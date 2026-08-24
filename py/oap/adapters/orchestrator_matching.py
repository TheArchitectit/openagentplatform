"""
Adapter skill-overlap matching used by the orchestration service.

Contains the pure scoring / selection logic that ``OrchestrationService``
uses to route incoming tasks to the best-matching registered adapter.  The
logic is provided as a mixin so the service can inherit it while keeping its
own public API readable.
"""

from __future__ import annotations

from oap.adapters.errors import FrameworkNotFoundError


class AdapterMatchingMixin:
    """Skill-overlap scoring and adapter selection.

    The mixin relies on instance attributes provided by
    ``OrchestrationService``: ``_adapters`` (registry key -> wrapper) and
    ``_adapter_cards`` (registry key -> AgentCard).
    """

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
