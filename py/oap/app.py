"""
FastAPI application factory for the OAP adapter subsystem.

Provides lifespan-managed startup/shutdown that warms the process pool and
registers all installed adapters, plus the full REST API for adapter
discovery, invocation, streaming, cancellation, and cost management.
"""

from __future__ import annotations

from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from typing import Any

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

# -- Adapter module imports -------------------------------------------------
# The orchestrator registers whatever classes have fired @register_adapter at
# import time. Importing the adapter modules here is what populates
# ADAPTER_REGISTRY; without these imports the registry is empty and every
# /api/v1/adapters response lists zero adapters. All framework dependencies
# (langgraph, crewai, ...) are imported lazily inside each wrapper's start()
# so importing is safe without those packages installed.
from oap.adapters.anthropic_adapter import AnthropicAdapter  # noqa: F401
from oap.adapters.api import router as adapters_router
from oap.adapters.autogen_adapter import AutoGenAdapter  # noqa: F401
from oap.adapters.cost import CostManager
from oap.adapters.crewai_adapter import CrewAIAdapter  # noqa: F401
from oap.adapters.langgraph_adapter import LangGraphAdapter  # noqa: F401
from oap.adapters.openai_adapter import OpenAIAgentsAdapter  # noqa: F401
from oap.adapters.orchestrator import AdapterInfo, OrchestrationService
from oap.adapters.ozore_adapter import OzoreAdapter  # noqa: F401
from oap.adapters.semantic_kernel_adapter import SemanticKernelAdapter  # noqa: F401
from oap.adapters.types import HealthStatus

# ---------------------------------------------------------------------------
# Application factory
# ---------------------------------------------------------------------------


def create_app() -> FastAPI:
    """Build and return a configured FastAPI application.

    The returned app owns a single ``OrchestrationService`` instance stored
    on ``app.state.orchestrator`` and a ``CostManager`` on
    ``app.state.cost_manager``.
    """

    @asynccontextmanager
    async def lifespan(app: FastAPI) -> AsyncIterator[None]:
        orchestrator = OrchestrationService()
        cost_manager = CostManager()
        app.state.orchestrator = orchestrator
        app.state.cost_manager = cost_manager
        await orchestrator.start()
        try:
            yield
        finally:
            await orchestrator.stop()

    app = FastAPI(title="OAP Adapter Service", version="0.1.0", lifespan=lifespan)

    # -- CORS middleware ----------------------------------------------------

    app.add_middleware(
        CORSMiddleware,
        allow_origins=["http://localhost:5173"],
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    # -- Register API routes ------------------------------------------------

    app.include_router(adapters_router)

    # -- Legacy / non-versioned aliases for backwards compatibility ---------

    @app.get("/adapters", response_model=list[AdapterInfo])
    async def list_adapters() -> list[AdapterInfo]:
        """List all installed adapters with health status."""
        orchestrator: OrchestrationService = app.state.orchestrator
        return orchestrator.supported_adapters()

    @app.get("/adapters/{name}/health", response_model=HealthStatus)
    async def adapter_health(name: str) -> HealthStatus:
        """Health check for a single adapter by registry name."""
        orchestrator: OrchestrationService = app.state.orchestrator
        return await orchestrator.adapter_health(name)

    @app.get("/health")
    async def overall_health() -> dict[str, Any]:
        """Pool + orchestrator health summary."""
        orchestrator: OrchestrationService = app.state.orchestrator
        pool = orchestrator.pool
        adapters = orchestrator.supported_adapters()
        return {
            "status": "ok" if all(a.healthy for a in adapters) else "degraded",
            "pool": {
                "total_processes": pool.total_processes,
                "max_processes": pool.config.max_processes,
                "warm_adapter_count": pool.config.warm_adapter_count,
            },
            "adapters": [{"name": a.name, "healthy": a.healthy} for a in adapters],
        }

    return app


# Module-level app for ``uvicorn oap.app:app --port 8001``.
app = create_app()
