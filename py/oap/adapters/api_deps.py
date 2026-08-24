"""
FastAPI dependency providers for the adapter REST API.

The dependency functions read the application-level services off
``request.app.state`` so the router in ``api`` stays thin and testable.
"""

from __future__ import annotations

from fastapi import HTTPException, Request

from oap.adapters.cost import CostManager
from oap.adapters.orchestrator import OrchestrationService


def get_orchestrator(request: Request) -> OrchestrationService:
    """Return the application-level OrchestrationService from app.state."""
    orchestrator: OrchestrationService | None = getattr(request.app.state, "orchestrator", None)
    if orchestrator is None:
        raise HTTPException(status_code=503, detail="Orchestrator not initialised")
    return orchestrator


def get_cost_manager(request: Request) -> CostManager:
    """Return the application-level CostManager from app.state.

    Falls back to creating a new CostManager with default models if none
    is registered on app.state. This keeps the API functional even if the
    cost subsystem has not been wired into the lifespan.
    """
    cost_manager: CostManager | None = getattr(request.app.state, "cost_manager", None)
    if cost_manager is None:
        cost_manager = CostManager()
    return cost_manager
