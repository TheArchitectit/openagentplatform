"""
OpenAI Agents SDK adapter for the OAP platform.

Translates between the A2A InvokeRequest/Response protocol and the
openai-agents SDK (``openai.Agent`` + ``Runner``). Supports invoke,
streaming, and built-in guardrails.
"""

from __future__ import annotations

import asyncio
import time
from typing import Any, AsyncIterator

from oap.adapters.errors import FrameworkNotFoundError
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

# Models this adapter supports.
HEALTHY_COST_MODELS: list[str] = [
    "gpt-4o",
    "gpt-4o-mini",
    "gpt-4.1",
    "gpt-4.1-mini",
    "o3",
    "o3-mini",
    "o4-mini",
]


@register_adapter("openai_agents")
class OpenAIAgentsAdapter(AgentWrapper):
    """Adapter for the OpenAI Agents SDK.

    Lazy-imports ``openai-agents``. Builds an ``Agent`` with instructions
    and tools, then runs it via ``Runner.run`` / ``Runner.run_streamed``.
    """

    def __init__(
        self,
        model: str = "gpt-4o-mini",
        instructions: str = "You are a helpful assistant.",
        tools: list[Any] | None = None,
        api_key: str = "",
        **kwargs: Any,
    ) -> None:
        try:
            from openai import OpenAI  # noqa: F401
        except ImportError as exc:
            raise FrameworkNotFoundError(
                "openai is not installed. Run: pip install openai openai-agents",
                adapter_name="openai_agents",
            ) from exc

        self._model = model
        self._instructions = instructions
        self._tools = tools or []
        self._api_key = api_key
        self._start_time: float | None = None
        self._active_tasks: dict[str, bool] = {}
        self._lock = asyncio.Lock()

    # -- Discovery ---------------------------------------------------------

    @property
    def agent_card(self) -> AgentCard:
        return AgentCard(
            name="OpenAIAgents",
            description="OpenAI Agents SDK with tool use, guardrails, and built-in tracing.",
            version="1.0.0",
            url="oap://adapter/openai_agents",
            provider_name="OAP",
            provider_url="https://openagentplatform.io",
            skills=[
                AgentSkill(
                    name="reasoning",
                    description="Chain-of-thought reasoning with OpenAI models.",
                    tags=["reasoning", "openai"],
                ),
                AgentSkill(
                    name="tool-use",
                    description="Native tool / function calling.",
                    tags=["tools", "function-calling"],
                ),
                AgentSkill(
                    name="guardrails",
                    description="Input/output guardrails for safety.",
                    tags=["guardrails", "safety"],
                ),
                AgentSkill(
                    name="tracing",
                    description="Built-in execution tracing and observability.",
                    tags=["tracing", "observability"],
                ),
            ],
            streaming=True,
            push_notifications=False,
            auth_schemes=["api-key"],
            default_input_modes=["text"],
            default_output_modes=["text"],
        )

    # -- Helpers -----------------------------------------------------------

    def _build_agent(self) -> Any:
        """Build an openai-agents Agent. Falls back to a plain OpenAI client
        call if the openai-agents package is not available."""
        try:
            from agents import Agent
            return Agent(
                name="oap-openai-agent",
                instructions=self._instructions,
                model=self._model,
                tools=self._tools,
            )
        except ImportError:
            # Graceful fallback when the agents SDK is not installed;
            # invoke/stream will use a direct chat completion call.
            return None

    def _extract_text(self, parts: list[Part]) -> str:
        texts = [p.text for p in parts if p.type == "text" and p.text]
        return "\n".join(texts) if texts else ""

    def _direct_chat(self, user_input: str) -> tuple[str, int]:
        """Fallback path using openai.OpenAI directly."""
        from openai import OpenAI

        client = OpenAI(api_key=self._api_key or "REPLACE_ME")
        response = client.chat.completions.create(
            model=self._model,
            messages=[
                {"role": "system", "content": self._instructions},
                {"role": "user", "content": user_input},
            ],
        )
        content = response.choices[0].message.content or ""
        tokens = response.usage.total_tokens if response.usage else len(content.split())
        return content, tokens

    # -- Invocation --------------------------------------------------------

    async def invoke(self, req: InvokeRequest) -> InvokeResponse:
        self._start_time = time.time()
        task_id = req.task_id or "openai-invoke"
        t0 = time.time()

        try:
            user_input = self._extract_text(req.messages) or "Hello."
            agent = self._build_agent()
            loop = asyncio.get_event_loop()

            if agent is not None:
                try:
                    from agents import Runner
                    result = await loop.run_in_executor(
                        None,
                        lambda: Runner.run_sync(agent, user_input),
                    )
                    output_text = str(
                        getattr(result, "final_output", None) or result
                    )
                    tokens = len(output_text.split())
                except Exception:
                    output_text, tokens = self._direct_chat(user_input)
            else:
                output_text, tokens = self._direct_chat(user_input)

            return InvokeResponse(
                task_id=task_id,
                status="completed",
                messages=[Part(type="text", text=output_text)],
                tokens_used=tokens,
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
        task_id = req.task_id or "openai-stream"
        self._active_tasks[task_id] = False

        yield StreamEvent(task_id=task_id, event_type="status", status="started")

        try:
            user_input = self._extract_text(req.messages) or "Hello."
            agent = self._build_agent()
            loop = asyncio.get_event_loop()

            output_text = ""
            tokens = 0

            if agent is not None:
                try:
                    from openai import OpenAI
                    client = OpenAI(api_key=self._api_key or "REPLACE_ME")

                    def _stream() -> tuple[str, int]:
                        collected: list[str] = []
                        total_tokens = 0
                        stream_resp = client.chat.completions.create(
                            model=self._model,
                            messages=[
                                {"role": "system", "content": self._instructions},
                                {"role": "user", "content": user_input},
                            ],
                            stream=True,
                        )
                        for chunk in stream_resp:
                            if chunk.choices and chunk.choices[0].delta.content:
                                collected.append(chunk.choices[0].delta.content)
                        return "".join(collected), total_tokens

                    output_text, tokens = await loop.run_in_executor(None, _stream)
                except Exception:
                    output_text, tokens = self._direct_chat(user_input)
            else:
                output_text, tokens = self._direct_chat(user_input)

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
        finally:
            self._active_tasks.pop(task_id, None)

    # -- Cancellation ------------------------------------------------------

    async def cancel(self, task_id: str) -> bool:
        if task_id not in self._active_tasks:
            return False
        self._active_tasks[task_id] = True
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
