"""
Pydantic request/response models for the adapter REST API.

These models define the wire-level contract between HTTP clients and the
OAP adapter service. Internal types from ``oap.adapters.types`` are
translated to/from these models at the API boundary.
"""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field

from oap.adapters.types import AgentCard, HealthStatus, InvokeResponse, Part, StreamEvent


# ---------------------------------------------------------------------------
# Request models
# ---------------------------------------------------------------------------


class InvokeRequestModel(BaseModel):
    """Request body for POST /api/v1/adapters/invoke.

    Attributes:
        adapter_name: Registry name of the preferred adapter.
        task_id: Caller-supplied or auto-generated task identifier.
        messages: Ordered list of message Parts.
        metadata: Arbitrary caller-supplied metadata (routing hints, etc.).
        timeout: Maximum wall-clock seconds for the invocation.
    """

    adapter_name: str = ""
    task_id: str = ""
    messages: list[Part] = Field(default_factory=list)
    metadata: dict[str, Any] = Field(default_factory=dict)
    timeout: float = 120.0


class StreamRequestModel(BaseModel):
    """Request body for POST /api/v1/adapters/stream.

    Same shape as InvokeRequestModel — streaming is determined by the
    endpoint rather than the body.
    """

    adapter_name: str = ""
    task_id: str = ""
    messages: list[Part] = Field(default_factory=list)
    metadata: dict[str, Any] = Field(default_factory=dict)
    timeout: float = 120.0


# ---------------------------------------------------------------------------
# Response models
# ---------------------------------------------------------------------------


class AdapterListEntry(BaseModel):
    """Single entry in the adapter listing response.

    Attributes:
        name: Registry name of the adapter.
        agent_card: The adapter's AgentCard.
        healthy: Whether the adapter is currently healthy.
    """

    name: str
    agent_card: AgentCard
    healthy: bool = True


class AdapterListResponse(BaseModel):
    """Response for GET /api/v1/adapters.

    Attributes:
        adapters: List of all registered adapters.
    """

    adapters: list[AdapterListEntry] = Field(default_factory=list)


class AdapterCardResponse(BaseModel):
    """Response for GET /api/v1/adapters/{name}/card.

    Attributes:
        name: Registry name of the adapter.
        agent_card: The adapter's AgentCard.
    """

    name: str
    agent_card: AgentCard


class AdapterHealthResponse(BaseModel):
    """Response for GET /api/v1/adapters/{name}/health.

    Attributes:
        name: Registry name of the adapter.
        health: The adapter's health status.
    """

    name: str
    health: HealthStatus


class ModelEntry(BaseModel):
    """A single cost model entry in the models listing.

    Attributes:
        model_name: Canonical model identifier.
        provider: Provider name.
        input_per_1k: Cost in USD per 1 000 input tokens.
        output_per_1k: Cost in USD per 1 000 output tokens.
        currency: ISO 4217 currency code.
    """

    model_name: str
    provider: str
    input_per_1k: float
    output_per_1k: float
    currency: str = "USD"


class ModelsResponse(BaseModel):
    """Response for GET /api/v1/adapters/{name}/models.

    Note: The {name} path parameter is accepted for API consistency but
    cost models are global (not per-adapter). The adapter name is echoed
    in the response.

    Attributes:
        adapter_name: Echoed adapter name from the path.
        models: List of supported cost models.
    """

    adapter_name: str
    models: list[ModelEntry] = Field(default_factory=list)


class CancelResponse(BaseModel):
    """Response for POST /api/v1/adapters/{task_id}/cancel.

    Attributes:
        task_id: The task identifier that was cancelled.
        cancelled: True if cancellation was delivered.
    """

    task_id: str
    cancelled: bool


class CostUsageResponse(BaseModel):
    """Response for GET /api/v1/cost/usage.

    Wraps the UsageReport from the CostManager.

    Attributes:
        report: The usage report data.
    """

    report: dict[str, Any] = Field(default_factory=dict)


class BudgetEntry(BaseModel):
    """A single budget entry in the budget status response.

    Attributes:
        org_id: Organisation identifier.
        monthly_limit: Configured monthly budget in USD.
        current_spend: Cumulative spend for the current period.
        currency: ISO 4217 currency code.
    """

    org_id: str
    monthly_limit: float
    current_spend: float
    currency: str = "USD"


class BudgetResponse(BaseModel):
    """Response for GET /api/v1/cost/budgets.

    Attributes:
        budgets: List of budget entries per organisation.
    """

    budgets: list[BudgetEntry] = Field(default_factory=list)


class ErrorResponse(BaseModel):
    """Standard error response body.

    Attributes:
        detail: Human-readable error description.
    """

    detail: str
