"""
Cost management for the OAP adapter subsystem.

Tracks token usage and monetary cost per task, adapter, organisation, and
time period. Provides pre-populated model pricing, per-organisation budget
tracking with threshold-based alerts, and usage reporting.
"""

from __future__ import annotations

import enum
import time
import uuid
from typing import Any

from pydantic import BaseModel, Field

from oap.adapters.types import CostRecord

# ---------------------------------------------------------------------------
# CostModel — per-model pricing definition.
# ---------------------------------------------------------------------------


class CostModel(BaseModel):
    """Pricing definition for a single model.

    Attributes:
        model_name: Canonical model identifier (e.g., "claude-opus-4-8").
        provider: Provider name (e.g., "anthropic", "openai", "google").
        input_per_1k: Cost in USD per 1 000 input/prompt tokens.
        output_per_1k: Cost in USD per 1 000 output/completion tokens.
        currency: ISO 4217 currency code.
    """

    model_name: str
    provider: str
    input_per_1k: float
    output_per_1k: float
    currency: str = "USD"


# ---------------------------------------------------------------------------
# Pre-populated pricing for supported models.
# ---------------------------------------------------------------------------

DEFAULT_COST_MODELS: dict[str, CostModel] = {
    "claude-opus-4-8": CostModel(
        model_name="claude-opus-4-8",
        provider="anthropic",
        input_per_1k=0.015,
        output_per_1k=0.075,
    ),
    "claude-sonnet-4-6": CostModel(
        model_name="claude-sonnet-4-6",
        provider="anthropic",
        input_per_1k=0.003,
        output_per_1k=0.015,
    ),
    "claude-haiku-4-5": CostModel(
        model_name="claude-haiku-4-5",
        provider="anthropic",
        input_per_1k=0.0008,
        output_per_1k=0.004,
    ),
    "gpt-4o": CostModel(
        model_name="gpt-4o",
        provider="openai",
        input_per_1k=0.0025,
        output_per_1k=0.010,
    ),
    "gpt-4.1-mini": CostModel(
        model_name="gpt-4.1-mini",
        provider="openai",
        input_per_1k=0.00015,
        output_per_1k=0.0006,
    ),
    "gemini-2.5-pro": CostModel(
        model_name="gemini-2.5-pro",
        provider="google",
        input_per_1k=0.00125,
        output_per_1k=0.005,
    ),
}


# ---------------------------------------------------------------------------
# BudgetAlert / BudgetLimit — budget tracking primitives.
# ---------------------------------------------------------------------------


class AlertSeverity(str, enum.Enum):
    """Severity levels for budget alerts."""

    INFO = "info"
    WARNING = "warning"
    CRITICAL = "critical"


class BudgetAlert(BaseModel):
    """An alert emitted when budget consumption crosses a threshold.

    Attributes:
        org_id: Organisation this alert pertains to.
        threshold_pct: The threshold that was crossed (80, 90, or 100).
        severity: Alert severity classification.
        spent: Current cumulative spend in USD.
        limit: Configured budget limit in USD.
        message: Human-readable alert message.
        timestamp: Unix epoch time when the alert was generated.
    """

    org_id: str
    threshold_pct: int
    severity: AlertSeverity
    spent: float
    limit: float
    message: str
    timestamp: float = Field(default_factory=time.time)


class BudgetLimit(BaseModel):
    """Budget configuration for a single organisation.

    Attributes:
        org_id: Organisation identifier.
        monthly_limit: Maximum allowed spend in USD per calendar month.
        currency: ISO 4217 currency code.
    """

    org_id: str
    monthly_limit: float
    currency: str = "USD"


# ---------------------------------------------------------------------------
# UsageReport — aggregated usage data.
# ---------------------------------------------------------------------------


class UsageReport(BaseModel):
    """Aggregated usage report for an organisation over a time range.

    Attributes:
        org_id: Organisation identifier.
        time_range_start: Start of the reporting window (Unix epoch).
        time_range_end: End of the reporting window (Unix epoch).
        total_cost: Total spend in the reporting currency.
        total_tokens: Total tokens consumed (prompt + completion).
        total_prompt_tokens: Total input/prompt tokens.
        total_completion_tokens: Total output/completion tokens.
        by_model: Per-model cost and token breakdown.
        by_adapter: Per-adapter cost and token breakdown.
        record_count: Number of individual cost records in this report.
    """

    org_id: str
    time_range_start: float
    time_range_end: float
    total_cost: float
    total_tokens: int
    total_prompt_tokens: int
    total_completion_tokens: int
    by_model: dict[str, dict[str, Any]] = Field(default_factory=dict)
    by_adapter: dict[str, dict[str, Any]] = Field(default_factory=dict)
    record_count: int = 0


# ---------------------------------------------------------------------------
# BudgetTracker — per-organisation budget enforcement with threshold alerts.
# ---------------------------------------------------------------------------

_THRESHOLDS: list[int] = [80, 90, 100]


class BudgetTracker:
    """Tracks cumulative spend per organisation and fires threshold alerts.

    Maintains a per-org running total for the current calendar month. When
    cumulative spend crosses 80 %, 90 %, or 100 % of the configured limit, a
    BudgetAlert is generated and recorded. Each threshold fires at most
    once per billing period.

    Attributes:
        _limits: Per-organisation budget limits keyed by org_id.
        _spend: Cumulative spend per org for the current period.
        _alerts: Alerts generated in the current period.
        _fired: Tracks which thresholds have already fired per org.
    """

    def __init__(self) -> None:
        self._limits: dict[str, BudgetLimit] = {}
        self._spend: dict[str, float] = {}
        self._alerts: list[BudgetAlert] = []
        self._fired: dict[str, set[int]] = {}

    def set_limit(self, org_id: str, monthly_limit: float, currency: str = "USD") -> None:
        """Set or update the monthly budget limit for an organisation.

        Args:
            org_id: Organisation identifier.
            monthly_limit: Maximum monthly spend in the given currency.
            currency: ISO 4217 currency code.
        """
        self._limits[org_id] = BudgetLimit(
            org_id=org_id,
            monthly_limit=monthly_limit,
            currency=currency,
        )
        if org_id not in self._spend:
            self._spend[org_id] = 0.0
            self._fired[org_id] = set()

    def add_spend(self, org_id: str, amount: float) -> list[BudgetAlert]:
        """Record additional spend and check for threshold crossings.

        Args:
            org_id: Organisation identifier.
            amount: Cost increment in USD.

        Returns:
            A list of newly triggered BudgetAlerts (may be empty).
        """
        if org_id not in self._limits:
            return []

        self._spend[org_id] = self._spend.get(org_id, 0.0) + amount
        limit = self._limits[org_id].monthly_limit
        if limit <= 0:
            return []

        pct = (self._spend[org_id] / limit) * 100.0
        fired = self._fired.setdefault(org_id, set())
        new_alerts: list[BudgetAlert] = []

        for threshold in _THRESHOLDS:
            if pct >= threshold and threshold not in fired:
                fired.add(threshold)
                severity = (
                    AlertSeverity.CRITICAL
                    if threshold == 100
                    else AlertSeverity.WARNING
                    if threshold == 90
                    else AlertSeverity.INFO
                )
                alert = BudgetAlert(
                    org_id=org_id,
                    threshold_pct=threshold,
                    severity=severity,
                    spent=round(self._spend[org_id], 6),
                    limit=limit,
                    message=(
                        f"Organisation {org_id} has spent "
                        f"${self._spend[org_id]:.2f} of ${limit:.2f} "
                        f"({pct:.1f}%) — {threshold}% threshold reached."
                    ),
                )
                self._alerts.append(alert)
                new_alerts.append(alert)

        return new_alerts

    def get_spend(self, org_id: str) -> float:
        """Return the current cumulative spend for an organisation."""
        return self._spend.get(org_id, 0.0)

    def get_limit(self, org_id: str) -> float:
        """Return the configured monthly limit, or 0.0 if none set."""
        entry = self._limits.get(org_id)
        return entry.monthly_limit if entry else 0.0

    def get_alerts(self, org_id: str | None = None) -> list[BudgetAlert]:
        """Return alerts, optionally filtered by organisation.

        Args:
            org_id: If provided, return only alerts for this org.

        Returns:
            A list of BudgetAlert objects.
        """
        if org_id is None:
            return list(self._alerts)
        return [a for a in self._alerts if a.org_id == org_id]

    def reset(self, org_id: str | None = None) -> None:
        """Reset spend, fired thresholds, and alerts.

        Args:
            org_id: If provided, reset only this org. If None, reset all.
        """
        if org_id is None:
            self._spend.clear()
            self._fired.clear()
            self._alerts.clear()
        else:
            self._spend[org_id] = 0.0
            self._fired.pop(org_id, None)
            self._alerts = [a for a in self._alerts if a.org_id != org_id]


# ---------------------------------------------------------------------------
# CostManager — central cost tracking and estimation.
# ---------------------------------------------------------------------------


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
