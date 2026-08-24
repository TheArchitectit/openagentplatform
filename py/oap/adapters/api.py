# ruff: noqa: B008
"""
REST API for the OAP adapter subsystem.

Exposes the OrchestrationService and CostManager over HTTP:

  POST /api/v1/adapters/invoke                      -- invoke adapter (req/resp)
  POST /api/v1/adapters/stream                       -- invoke adapter (SSE stream)
  POST /api/v1/adapters/{task_id}/cancel             -- cancel an in-flight task
  GET  /api/v1/adapters                              -- list all adapters
  GET  /api/v1/adapters/{name}/card                  -- AgentCard for an adapter
  GET  /api/v1/adapters/{name}/health                -- health for an adapter
  GET  /api/v1/adapters/{name}/models                -- supported cost models
  GET  /api/v1/cost/usage                            -- usage report
  GET  /api/v1/cost/budgets                          -- budget status

The API is a FastAPI ``APIRouter`` designed to be mounted on the main
application by ``oap.app``.  Dependency providers and shared request
builders live in ``api_deps`` and ``api_util``.
"""

from __future__ import annotations

import time
import uuid
from collections.abc import AsyncIterator

from fastapi import APIRouter, Depends, HTTPException, Query
from fastapi.responses import StreamingResponse

from oap.adapters.api_deps import get_cost_manager, get_orchestrator
from oap.adapters.api_models import (
    AdapterCardResponse,
    AdapterHealthResponse,
    AdapterListEntry,
    AdapterListResponse,
    BudgetEntry,
    BudgetResponse,
    CancelResponse,
    CostUsageResponse,
    InvokeRequestModel,
    ModelEntry,
    ModelsResponse,
    StreamRequestModel,
)
from oap.adapters.api_util import build_invoke_request
from oap.adapters.cost import CostManager
from oap.adapters.orchestrator import AdapterInfo, OrchestrationService
from oap.adapters.types import (
    HealthStatus,
    InvokeResponse,
    StreamEvent,
)

# ---------------------------------------------------------------------------
# Router
# ---------------------------------------------------------------------------

router = APIRouter(prefix="/api/v1", tags=["adapters"])


# ---------------------------------------------------------------------------
# POST /api/v1/adapters/invoke
# ---------------------------------------------------------------------------


@router.post("/adapters/invoke", response_model=InvokeResponse)
async def invoke_adapter(
    body: InvokeRequestModel,
    orchestrator: OrchestrationService = Depends(get_orchestrator),
) -> InvokeResponse:
    """Invoke an adapter and return the terminal response.

    The ``adapter_name`` field in the body is used as the preferred agent
    for routing.  If empty, the orchestrator will pick the best-matching
    adapter based on skill overlap.
    """
    task_id = body.task_id or str(uuid.uuid4())
    request = build_invoke_request(task_id, body)

    return await orchestrator.handle_task(task_id, request)


# ---------------------------------------------------------------------------
# POST /api/v1/adapters/stream  (SSE)
# ---------------------------------------------------------------------------


@router.post("/adapters/stream")
async def stream_adapter(
    body: StreamRequestModel,
    orchestrator: OrchestrationService = Depends(get_orchestrator),
) -> StreamingResponse:
    """Invoke an adapter and stream events as Server-Sent Events.

    Each ``StreamEvent`` is serialised as a JSON ``data:`` line.  The
    stream terminates with a final ``data: [DONE]`` line.
    """
    task_id = body.task_id or str(uuid.uuid4())
    request = build_invoke_request(task_id, body)

    async def event_generator() -> AsyncIterator[str]:
        try:
            async for event in orchestrator.handle_streaming_task(task_id, request):
                yield f"data: {event.model_dump_json()}\n\n"
        except Exception as exc:
            error_event = StreamEvent(
                task_id=task_id,
                event_type="error",
                status="error",
                metadata={"error": str(exc)},
            )
            yield f"data: {error_event.model_dump_json()}\n\n"
        finally:
            yield "data: [DONE]\n\n"

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",
        },
    )


# ---------------------------------------------------------------------------
# POST /api/v1/adapters/{task_id}/cancel
# ---------------------------------------------------------------------------


@router.post("/adapters/{task_id}/cancel", response_model=CancelResponse)
async def cancel_task(
    task_id: str,
    orchestrator: OrchestrationService = Depends(get_orchestrator),
) -> CancelResponse:
    """Cancel an in-flight task by task_id."""
    cancelled = await orchestrator.cancel_task(task_id)
    return CancelResponse(task_id=task_id, cancelled=cancelled)


# ---------------------------------------------------------------------------
# GET /api/v1/adapters
# ---------------------------------------------------------------------------


@router.get("/adapters", response_model=AdapterListResponse)
async def list_adapters(
    orchestrator: OrchestrationService = Depends(get_orchestrator),
) -> AdapterListResponse:
    """List all registered adapters with their AgentCards and health status."""
    adapters: list[AdapterInfo] = orchestrator.supported_adapters()
    entries = [
        AdapterListEntry(
            name=a.name,
            agent_card=a.agent_card,
            healthy=a.healthy,
        )
        for a in adapters
    ]
    return AdapterListResponse(adapters=entries)


# ---------------------------------------------------------------------------
# GET /api/v1/adapters/{name}/card
# ---------------------------------------------------------------------------


@router.get("/adapters/{name}/card", response_model=AdapterCardResponse)
async def get_adapter_card(
    name: str,
    orchestrator: OrchestrationService = Depends(get_orchestrator),
) -> AdapterCardResponse:
    """Return the AgentCard for a single adapter."""
    for info in orchestrator.supported_adapters():
        if info.name == name:
            return AdapterCardResponse(name=info.name, agent_card=info.agent_card)
    raise HTTPException(status_code=404, detail=f"Adapter '{name}' not found")


# ---------------------------------------------------------------------------
# GET /api/v1/adapters/{name}/health
# ---------------------------------------------------------------------------


@router.get("/adapters/{name}/health", response_model=AdapterHealthResponse)
async def get_adapter_health(
    name: str,
    orchestrator: OrchestrationService = Depends(get_orchestrator),
) -> AdapterHealthResponse:
    """Return the health status for a single adapter."""
    health: HealthStatus = await orchestrator.adapter_health(name)
    if not health.healthy and "not found" in health.last_error:
        raise HTTPException(status_code=404, detail=f"Adapter '{name}' not found")
    return AdapterHealthResponse(name=name, health=health)


# ---------------------------------------------------------------------------
# GET /api/v1/adapters/{name}/models
# ---------------------------------------------------------------------------


@router.get("/adapters/{name}/models", response_model=ModelsResponse)
async def get_adapter_models(
    name: str,
    cost_manager: CostManager = Depends(get_cost_manager),
) -> ModelsResponse:
    """Return supported cost models.

    Cost models are global (not per-adapter), but the ``name`` path
    parameter is accepted for API consistency.
    """
    models = [
        ModelEntry(
            model_name=cm.model_name,
            provider=cm.provider,
            input_per_1k=cm.input_per_1k,
            output_per_1k=cm.output_per_1k,
            currency=cm.currency,
        )
        for cm in cost_manager._models.values()
    ]
    return ModelsResponse(adapter_name=name, models=models)


# ---------------------------------------------------------------------------
# GET /api/v1/cost/usage
# ---------------------------------------------------------------------------


@router.get("/cost/usage", response_model=CostUsageResponse)
async def get_cost_usage(
    org_id: str = Query("", description="Organisation ID to filter by"),
    from_ts: float = Query(0.0, alias="from", description="Start of time range (Unix epoch)"),
    to_ts: float | None = Query(None, alias="to", description="End of time range (Unix epoch)"),
    cost_manager: CostManager = Depends(get_cost_manager),
) -> CostUsageResponse:
    """Return a usage report for the given organisation and time range."""
    end = to_ts if to_ts is not None else time.time()
    report = cost_manager.get_usage(org_id, {"start": from_ts, "end": end})
    return CostUsageResponse(report=report.model_dump())


# ---------------------------------------------------------------------------
# GET /api/v1/cost/budgets
# ---------------------------------------------------------------------------


@router.get("/cost/budgets", response_model=BudgetResponse)
async def get_cost_budgets(
    cost_manager: CostManager = Depends(get_cost_manager),
) -> BudgetResponse:
    """Return budget status for all configured organisations."""
    budget_tracker = cost_manager.budget
    entries: list[BudgetEntry] = []
    for org_id, limit in budget_tracker._limits.items():
        entries.append(
            BudgetEntry(
                org_id=org_id,
                monthly_limit=limit.monthly_limit,
                current_spend=budget_tracker.get_spend(org_id),
                currency=limit.currency,
            )
        )
    return BudgetResponse(budgets=entries)
