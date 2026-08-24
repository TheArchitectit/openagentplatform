"""
LangGraph adapter for the OAP platform.

Translates between the A2A InvokeRequest/Response protocol and the
LangGraph / LangChain framework. Supports both invoke and streaming
interactions against a compiled state graph.

The framework-specific graph / LLM construction lives in ``langgraph_graph``.
"""

from __future__ import annotations

import asyncio
import threading
import time
from typing import Any, AsyncIterator

from oap.adapters.errors import FrameworkNotFoundError, InvocationError
from oap.adapters.langgraph_graph import (
    HEALTHY_COST_MODELS,  # noqa: F401  (re-exported public constant)
    _build_graph,
    _build_llm,
    _parts_to_messages,
)
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


@register_adapter("langgraph")
class LangGraphAdapter(AgentWrapper):
    """Adapter for the LangGraph framework.

    Lazy-imports ``langgraph``, ``langchain_core``, and the provider-specific
    LangChat wrappers. Supports invoke, streaming, and cancellation of
    compiled state graphs.
    """

    def __init__(
        self,
        model: str = "gpt-4o-mini",
        system_message: str = "You are a helpful assistant.",
        temperature: float = 0.7,
        max_tokens: int = 4096,
        **kwargs: Any,
    ) -> None:
        try:
            from langgraph.graph import END, StateGraph  # noqa: F401
            from langchain_core.messages import AIMessage, HumanMessage  # noqa: F401
        except ImportError as exc:
            raise FrameworkNotFoundError(
                "langgraph is not installed. Run: pip install langgraph langchain-core",
                adapter_name="langgraph",
            ) from exc

        self._model = model
        self._system_message = system_message
        self._temperature = temperature
        self._max_tokens = max_tokens
        self._start_time: float | None = None
        self._active_tasks: dict[str, threading.Event] = {}
        self._lock = threading.Lock()

    # -- Discovery ---------------------------------------------------------

    @property
    def agent_card(self) -> AgentCard:
        return AgentCard(
            name="LangGraph",
            description="Stateful, multi-step agent graphs powered by LangGraph + LangChain.",
            version="1.0.0",
            url="oap://adapter/langgraph",
            provider_name="OAP",
            provider_url="https://openagentplatform.io",
            skills=[
                AgentSkill(
                    name="reasoning",
                    description="Chain-of-thought reasoning over complex inputs.",
                    tags=["reasoning", "chain-of-thought"],
                ),
                AgentSkill(
                    name="planning",
                    description="Multi-step task planning and decomposition.",
                    tags=["planning", "decomposition"],
                ),
                AgentSkill(
                    name="tool-use",
                    description="Tool/function calling within a graph node.",
                    tags=["tools", "function-calling"],
                ),
                AgentSkill(
                    name="multi-step",
                    description="Persistent stateful multi-step workflows.",
                    tags=["stateful", "graph", "multi-step"],
                ),
            ],
            streaming=True,
            push_notifications=False,
            auth_schemes=["api-key"],
            default_input_modes=["text"],
            default_output_modes=["text"],
        )

    # -- Build helpers (delegate to langgraph_graph) -----------------------

    def _build_llm(self) -> Any:
        """Construct the framework-native chat model for the configured provider."""
        return _build_llm(self._model, self._max_tokens, self._temperature)

    def _build_graph(self) -> Any:
        """Compile a minimal two-node graph (agent -> END)."""
        return _build_graph(self._model, self._max_tokens, self._temperature)

    def _parts_to_messages(self, parts: list[Part]) -> list[Any]:
        return _parts_to_messages(self._system_message, parts)

    def _register_task(self, task_id: str) -> threading.Event:
        ev = threading.Event()
        with self._lock:
            self._active_tasks[task_id] = ev
        return ev

    def _deregister_task(self, task_id: str) -> None:
        with self._lock:
            self._active_tasks.pop(task_id, None)

    # -- Invocation --------------------------------------------------------

    async def invoke(self, req: InvokeRequest) -> InvokeResponse:
        from langchain_core.messages import AIMessage

        self._start_time = time.time()
        task_id = req.task_id or "langgraph-invoke"
        cancel_event = self._register_task(task_id)
        t0 = time.time()

        try:
            graph = self._build_graph()
            messages = self._parts_to_messages(req.messages)
            state: dict = {"messages": messages}

            # Run graph invoke in a thread to respect cancellation.
            result_holder: dict = {"result": None, "error": None}

            def _run() -> None:
                try:
                    result_holder["result"] = graph.invoke(state)
                except Exception as e:
                    result_holder["error"] = e

            thread = threading.Thread(target=_run, daemon=True)
            thread.start()
            while thread.is_alive():
                if cancel_event.is_set():
                    return InvokeResponse(
                        task_id=task_id,
                        status="canceled",
                        messages=[],
                        duration_ms=int((time.time() - t0) * 1000),
                    )
                await asyncio.sleep(0.05)

            if result_holder["error"]:
                raise InvocationError(
                    str(result_holder["error"]), adapter_name="langgraph"
                )

            final_messages = result_holder["result"]["messages"]
            last = final_messages[-1] if final_messages else None
            content = last.content if isinstance(last, AIMessage) else str(last)

            tokens = sum(
                len(str(m.content).split())
                for m in final_messages
            )

            return InvokeResponse(
                task_id=task_id,
                status="completed",
                messages=[Part(type="text", text=str(content))],
                tokens_used=tokens,
                duration_ms=int((time.time() - t0) * 1000),
            )
        except FrameworkNotFoundError:
            raise
        except InvocationError:
            raise
        except Exception as exc:
            return InvokeResponse(
                task_id=task_id,
                status="failed",
                error_message=str(exc),
                duration_ms=int((time.time() - t0) * 1000),
            )
        finally:
            self._deregister_task(task_id)

    # -- Streaming ---------------------------------------------------------

    async def stream(self, req: InvokeRequest) -> AsyncIterator[StreamEvent]:
        task_id = req.task_id or "langgraph-stream"
        cancel_event = self._register_task(task_id)

        yield StreamEvent(task_id=task_id, event_type="status", status="started")

        try:
            graph = self._build_graph()
            messages = self._parts_to_messages(req.messages)
            state: dict = {"messages": messages}

            accumulated = ""
            async for event in graph.astream(state):
                if cancel_event.is_set():
                    yield StreamEvent(task_id=task_id, event_type="status", status="canceled")
                    return
                if isinstance(event, dict):
                    for _node, node_state in event.items():
                        if isinstance(node_state, dict) and "messages" in node_state:
                            from langchain_core.messages import AIMessage
                            last = node_state["messages"][-1]
                            if isinstance(last, AIMessage):
                                delta_text = str(last.content)
                                if delta_text and delta_text != accumulated:
                                    accumulated = delta_text
                                    yield StreamEvent(
                                        task_id=task_id,
                                        event_type="delta",
                                        delta=Part(type="text", text=delta_text),
                                    )

            yield StreamEvent(task_id=task_id, event_type="done")
        except Exception as exc:
            yield StreamEvent(
                task_id=task_id,
                event_type="error",
                metadata={"error": str(exc)},
            )
            yield StreamEvent(task_id=task_id, event_type="done")
        finally:
            self._deregister_task(task_id)

    # -- Cancellation ------------------------------------------------------

    async def cancel(self, task_id: str) -> bool:
        with self._lock:
            ev = self._active_tasks.get(task_id)
        if ev is None:
            return False
        ev.set()
        return True

    # -- Health ------------------------------------------------------------

    async def health(self) -> HealthStatus:
        uptime = 0.0
        if self._start_time is not None:
            uptime = time.time() - self._start_time
        with self._lock:
            active = len(self._active_tasks)
        return HealthStatus(
            healthy=True,
            uptime_seconds=uptime,
            active_tasks=active,
            memory_mb=0.0,
        )

    # -- Lifecycle ---------------------------------------------------------

    async def start(self) -> None:
        self._start_time = time.time()

    async def stop(self) -> None:
        with self._lock:
            for ev in self._active_tasks.values():
                ev.set()
            self._active_tasks.clear()
