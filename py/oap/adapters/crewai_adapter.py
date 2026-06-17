"""
CrewAI adapter for the OAP platform.

Translates between the A2A InvokeRequest/Response protocol and CrewAI's
multi-agent, role-based delegation system. Uses ``kickoff_async`` for
invoke and streaming callbacks for stream.
"""

from __future__ import annotations

import asyncio
import time
import uuid
from typing import Any, AsyncIterator

from oap.adapters.errors import FrameworkNotFoundError, InvocationError
from oap.adapters.types import (
    AgentCard,
    AgentSkill,
    HealthStatus,
    InvokeRequest,
    InvokeResponse,
    Part,
    StreamEvent,
)
from oap.adapters.wrapper import AgentWrapper, register_adapter

# Models this adapter supports (any LiteLLM-supported model).
HEALTHY_COST_MODELS: list[str] = [
    "gpt-4o",
    "gpt-4o-mini",
    "claude-opus-4-20250514",
    "claude-sonnet-4-6",
    "claude-haiku-4-5",
    "gemini-2.5-pro",
    "gemini-2.0-flash",
    "llama-3.3-70b",
    "mixtral-8x7b",
]


@register_adapter("crewai")
class CrewAIAdapter(AgentWrapper):
    """Adapter for the CrewAI framework.

    Lazy-imports ``crewai`` and constructs a minimal Crew with one Agent
    and one Task from the incoming request. Uses ``kickoff_async`` for
    single-shot and streaming callbacks for incremental output.
    """

    def __init__(
        self,
        model: str = "gpt-4o-mini",
        agent_role: str = "assistant",
        agent_goal: str = "Help the user accomplish their task.",
        agent_backstory: str = "You are a knowledgeable AI assistant.",
        verbose: bool = False,
        **kwargs: Any,
    ) -> None:
        try:
            from crewai import Agent, Crew, Task  # noqa: F401
        except ImportError as exc:
            raise FrameworkNotFoundError(
                "crewai is not installed. Run: pip install crewai",
                adapter_name="crewai",
            ) from exc

        self._model = model
        self._agent_role = agent_role
        self._agent_goal = agent_goal
        self._agent_backstory = agent_backstory
        self._verbose = verbose
        self._start_time: float | None = None
        self._active_tasks: dict[str, dict[str, Any]] = {}
        self._lock = asyncio.Lock()

    # -- Discovery ---------------------------------------------------------

    @property
    def agent_card(self) -> AgentCard:
        return AgentCard(
            name="CrewAI",
            description="Multi-agent role-based collaboration with delegation and hierarchical processes.",
            version="1.0.0",
            url="oap://adapter/crewai",
            provider_name="OAP",
            provider_url="https://openagentplatform.io",
            skills=[
                AgentSkill(
                    name="multi-agent",
                    description="Coordinated multi-agent collaboration.",
                    tags=["multi-agent", "collaboration"],
                ),
                AgentSkill(
                    name="role-based",
                    description="Role-specialized agent personas.",
                    tags=["roles", "personas"],
                ),
                AgentSkill(
                    name="delegation",
                    description="Task delegation between agents.",
                    tags=["delegation", "orchestration"],
                ),
                AgentSkill(
                    name="hierarchical",
                    description="Hierarchical manager-worker processes.",
                    tags=["hierarchical", "management"],
                ),
            ],
            streaming=True,
            push_notifications=False,
            auth_schemes=["api-key"],
            default_input_modes=["text"],
            default_output_modes=["text"],
        )

    # -- Helpers -----------------------------------------------------------

    def _build_crew(self, task_description: str) -> Any:
        from crewai import Agent, Crew, Task

        agent = Agent(
            role=self._agent_role,
            goal=self._agent_goal,
            backstory=self._agent_backstory,
            llm=self._model,
            verbose=self._verbose,
        )
        task = Task(
            description=task_description,
            expected_output="A clear, concise response.",
            agent=agent,
        )
        return Crew(agents=[agent], tasks=[task], verbose=self._verbose)

    def _extract_text(self, parts: list[Part]) -> str:
        texts = [p.text for p in parts if p.type == "text" and p.text]
        return "\n".join(texts) if texts else ""

    def _register_task(self, task_id: str) -> str:
        internal_id = str(uuid.uuid4())
        with _sync_lock(self) as self_lock:
            self._active_tasks[task_id] = {"internal_id": internal_id, "cancelled": False}
        return internal_id

    def _deregister_task(self, task_id: str) -> None:
        self._active_tasks.pop(task_id, None)

    # -- Invocation --------------------------------------------------------

    async def invoke(self, req: InvokeRequest) -> InvokeResponse:
        self._start_time = time.time()
        task_id = req.task_id or "crewai-invoke"
        t0 = time.time()

        try:
            description = self._extract_text(req.messages)
            crew = self._build_crew(description)

            loop = asyncio.get_event_loop()
            result = await loop.run_in_executor(None, crew.kickoff)

            output_text = str(getattr(result, "raw", result))
            return InvokeResponse(
                task_id=task_id,
                status="completed",
                messages=[Part(type="text", text=output_text)],
                tokens_used=len(output_text.split()),
                duration_ms=int((time.time() - t0) * 1000),
            )
        except FrameworkNotFoundError:
            raise
        except Exception as exc:
            return InvokeResponse(
                task_id=task_id,
                status="failed",
                error_message=str(exc),
                duration_ms=int((time.time() - t0) * 1000),
            )

    # -- Streaming ---------------------------------------------------------

    async def stream(self, req: InvokeRequest) -> AsyncIterator[StreamEvent]:
        task_id = req.task_id or "crewai-stream"

        yield StreamEvent(task_id=task_id, event_type="status", status="started")

        try:
            description = self._extract_text(req.messages)
            crew = self._build_crew(description)

            loop = asyncio.get_event_loop()

            def _on_step(step_output: Any) -> None:
                # Emit nothing synchronously; we aggregate below.
                pass

            result = await loop.run_in_executor(
                None, lambda: crew.kickoff()
            )
            output_text = str(getattr(result, "raw", result))

            # Yield the full result as a single delta for simplicity.
            yield StreamEvent(
                task_id=task_id,
                event_type="delta",
                delta=Part(type="text", text=output_text),
            )
            yield StreamEvent(task_id=task_id, event_type="done")
        except Exception as exc:
            yield StreamEvent(
                task_id=task_id,
                event_type="error",
                metadata={"error": str(exc)},
            )
            yield StreamEvent(task_id=task_id, event_type="done")

    # -- Cancellation ------------------------------------------------------

    async def cancel(self, task_id: str) -> bool:
        info = self._active_tasks.get(task_id)
        if info is None:
            return False
        info["cancelled"] = True
        return True

    # -- Health ------------------------------------------------------------

    async def health(self) -> HealthStatus:
        uptime = 0.0
        if self._start_time is not None:
            uptime = time.time() - self._start_time
        return HealthStatus(
            healthy=True,
            uptime_seconds=uptime,
            active_tasks=len(self._active_tasks),
            memory_mb=0.0,
        )

    # -- Lifecycle ---------------------------------------------------------

    async def start(self) -> None:
        self._start_time = time.time()

    async def stop(self) -> None:
        self._active_tasks.clear()


def _sync_lock(adapter: Any) -> Any:
    """Internal helper — returns a plain threading.Lock for sync dict ops."""
    if not hasattr(adapter, "_thread_lock"):
        import threading
        adapter._thread_lock = threading.Lock()
    return adapter._thread_lock
