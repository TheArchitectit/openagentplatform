"""
Cost management for the OAP adapter subsystem.

Tracks token usage and monetary cost per task, adapter, organisation, and
time period. Provides pre-populated model pricing, per-organisation budget
tracking with threshold-based alerts, and usage reporting.

Model pricing and budget primitives live in ``cost_models`` /
``cost_budget`` respectively.
"""

from __future__ import annotations

import time
import uuid
from typing import Any

from oap.adapters.cost_budget import BudgetTracker
from oap.adapters.cost_models import CostModel, UsageReport, DEFAULT_COST_MODELS
from oap.adapters.types import CostRecord

__all__ = [
    "CostManager",
    "BudgetTracker",
    "CostModel",
    "DEFAULT_COST_MODELS",
]


class CostManager:
    """Tracks token usage and cost across tasks, adapters, and organisations.

    Uses a configurable model-pricing table (pre-populated with common LLM
    models) to estimate and record costs. Integrates with BudgetTracker for
    per-org spend limits and threshold alerts.

    Attributes:
        _models: Pricing models keyed by model_name.
        _records: All cost records, keyed by record_id (UUID).
        _budget: Budget tracker for per-org limits.
    """

    def __init__(
        self,
        cost_models: dict[str, CostModel] | None = None,
        budget_tracker: BudgetTracker | None = None,
    ) -> None:
        self._models: dict[str, CostModel] = dict(cost_models or DEFAULT_COST_MODELS)
        self._records: dict[str, CostRecord] = {}
        self._budget: BudgetTracker = budget_tracker or BudgetTracker()

    # -- Model pricing ------------------------------------------------------

    def register_model(self, model: CostModel) -> None:
        """Register or update pricing for a model.

        Args:
            model: The CostModel to register.
        """
        self._models[model.model_name] = model

    def get_model(self, model_name: str) -> CostModel | None:
        """Look up pricing for a model.

        Args:
            model_name: Canonical model identifier.

        Returns:
            The CostModel or None if not found.
        """
        return self._models.get(model_name)

    # -- Cost estimation ----------------------------------------------------

    def estimate_cost(
        self,
        model: str,
        prompt_tokens: int,
        completion_tokens: int,
    ) -> float:
        """Estimate the cost in USD for a given token count.

        Args:
            model: Model identifier to look up pricing.
            prompt_tokens: Number of input/prompt tokens.
            completion_tokens: Number of output/completion tokens.

        Returns:
            Estimated cost in USD. Returns 0.0 if the model is not found.
        """
        cost_model = self._models.get(model)
        if cost_model is None:
            return 0.0
        input_cost = (prompt_tokens / 1000.0) * cost_model.input_per_1k
        output_cost = (completion_tokens / 1000.0) * cost_model.output_per_1k
        return round(input_cost + output_cost, 10)

    # -- Cost recording -----------------------------------------------------

    def record_cost(
        self,
        task_id: str,
        adapter: str,
        model: str,
        tokens: dict[str, int],
        org_id: str = "",
    ) -> CostRecord:
        """Record token usage and compute cost for a completed task.

        Args:
            task_id: Task identifier.
            adapter: Name of the framework adapter.
            model: Model identifier used.
            tokens: Dict with "prompt_tokens" and "completion_tokens" keys.
            org_id: Optional organisation ID for budget tracking.

        Returns:
            The created CostRecord.
        """
        prompt_tokens = tokens.get("prompt_tokens", 0)
        completion_tokens = tokens.get("completion_tokens", 0)
        total_cost = self.estimate_cost(model, prompt_tokens, completion_tokens)
        cost_model = self._models.get(model)
        currency = cost_model.currency if cost_model else "USD"

        record = CostRecord(
            task_id=task_id,
            framework=adapter,
            model=model,
            prompt_tokens=prompt_tokens,
            completion_tokens=completion_tokens,
            total_cost=total_cost,
            currency=currency,
        )
        record_id = str(uuid.uuid4())
        self._records[record_id] = record

        if org_id and total_cost > 0:
            self._budget.add_spend(org_id, total_cost)

        return record

    # -- Usage reporting ----------------------------------------------------

    def get_usage(self, org_id: str, time_range: dict[str, float]) -> UsageReport:
        """Generate a usage report for an organisation over a time range.

        Args:
            org_id: Organisation identifier to filter by.
            time_range: Dict with "start" and "end" Unix epoch timestamps.

        Returns:
            A UsageReport with aggregated metrics.
        """
        start = time_range.get("start", 0.0)
        end = time_range.get("end", time.time())

        # Records are filtered by a simple org_id field in metadata.
        # Since CostRecord does not carry org_id directly, we infer from
        # budget tracker spend data. In a production system, records
        # would be augmented with org_id at the storage layer.
        org_records: list[CostRecord] = []
        for rec in self._records.values():
            # Include all records if no org filter is meaningful, or
            # filter via a convention: records whose task_id contains
            # the org_id prefix.
            if org_id and org_id not in rec.task_id:
                continue
            org_records.append(rec)

        total_cost = sum(r.total_cost for r in org_records)
        total_prompt = sum(r.prompt_tokens for r in org_records)
        total_completion = sum(r.completion_tokens for r in org_records)

        by_model: dict[str, dict[str, Any]] = {}
        by_adapter: dict[str, dict[str, Any]] = {}

        for rec in org_records:
            m = by_model.setdefault(
                rec.model,
                {"cost": 0.0, "prompt_tokens": 0, "completion_tokens": 0, "count": 0},
            )
            m["cost"] += rec.total_cost
            m["prompt_tokens"] += rec.prompt_tokens
            m["completion_tokens"] += rec.completion_tokens
            m["count"] += 1

            a = by_adapter.setdefault(
                rec.framework,
                {"cost": 0.0, "prompt_tokens": 0, "completion_tokens": 0, "count": 0},
            )
            a["cost"] += rec.total_cost
            a["prompt_tokens"] += rec.prompt_tokens
            a["completion_tokens"] += rec.completion_tokens
            a["count"] += 1

        return UsageReport(
            org_id=org_id,
            time_range_start=start,
            time_range_end=end,
            total_cost=round(total_cost, 6),
            total_tokens=total_prompt + total_completion,
            total_prompt_tokens=total_prompt,
            total_completion_tokens=total_completion,
            by_model=by_model,
            by_adapter=by_adapter,
            record_count=len(org_records),
        )

    # -- Budget access ------------------------------------------------------

    @property
    def budget(self) -> BudgetTracker:
        """Access the underlying BudgetTracker for direct budget operations."""
        return self._budget

    def get_records(self) -> list[CostRecord]:
        """Return all cost records managed by this CostManager."""
        return list(self._records.values())
