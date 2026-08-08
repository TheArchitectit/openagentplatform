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
    # Ozore — default hosted LLM agent (account-specific pricing; these
    # are estimates that should be replaced with actual contract rates).
    "ozore/custom": CostModel(
        model_name="ozore/custom",
        provider="ozore",
        input_per_1k=0.0015,   # estimated — adjust to actual Ozore pricing
        output_per_1k=0.0045,  # estimated — adjust to actual Ozore pricing
    ),
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


