"""
AutoGen adapter for the OAP platform.

Translates between the A2A InvokeRequest/Response protocol and the
AutoGen (autogen-agentchat) conversational agent framework. Creates an
AssistantAgent + UserProxyAgent pair and runs a single chat turn.
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
    "o3",
    "o4-mini",
    "azure-gpt-4o",
    "azure-gpt-4o-mini",
]


@register_adapter("autogen")
class AutoGenAdapter(AgentWrapper):
    """Adapter for the AutoGen (pyautogen / autogen-agentchat) framework.

    Lazy-imports ``autogen``. Sets up an AssistantAgent with a
    UserProxyAgent and runs a single chat round.
    """

    def __init__(
        self,
        model: str = "gpt-4o-mini",
        system_message: str = "You are a helpful assistant.",
        api_key: str = "",
        base_url: str = "",
        **kwargs: Any,
    ) -> None:
        try:
            import autogen  # noqa: F401
        except ImportError as exc:
            raise FrameworkNotFoundError(
                "autogen is not installed. Run: pip install pyautogen",
                adapter_name="autogen",
            ) from exc

        self._model = model
        self._system_message = system_message
        self._api_key = api_key
        self._base_url = base_url
        self._start_time: float | None = None
        self._active_tasks: dict[str, bool] = {}
        self._lock = asyncio.Lock()

    # -- Discovery ---------------------------------------------------------

    @property
    def agent_card(self) -> AgentCard:
        return AgentCard(
            name="AutoGen",
            description="Conversational multi-agent framework with code generation and RAG support.",
            version="1.0.0",
            url="oap://adapter/autogen",
            provider_name="OAP",
            provider_url="https://openagentplatform.io",
            skills=[
                AgentSkill(
                    name="conversation",
                    description="Multi-turn conversational agents.",
                    tags=["conversation", "chat"],
                ),
                AgentSkill(
                    name="code-gen",
                    description="Automated code generation and execution.",
                    tags=["code", "execution"],
                ),
                AgentSkill(
                    name="multi-agent",
                    description="Coordinated multi-agent conversations.",
                    tags=["multi-agent"],
                ),
                AgentSkill(
                    name="RAG",
                    description="Retrieval-augmented generation workflows.",
                    tags=["rag", "retrieval"],
                ),
            ],
            streaming=True,
            push_notifications=False,
            auth_schemes=["api-key"],
            default_input_modes=["text"],
            default_output_modes=["text"],
        )

    # -- Helpers -----------------------------------------------------------

    def _build_config(self) -> dict:
        config: dict = {
            "model": self._model,
            "api_key": self._api_key or "REPLACE_ME",
        }
        if self._base_url:
            config["base_url"] = self._base_url
        return {"config_list": [config]}

    def _build_agents(self) -> tuple:
        import autogen

        llm_config = self._build_config()
        assistant = autogen.AssistantAgent(
            name="assistant",
            system_message=self._system_message,
            llm_config=llm_config,
        )
        user_proxy = autogen.UserProxyAgent(
            name="user_proxy",
            human_input_mode="NEVER",
            max_consecutive_auto_reply=1,
            llm_config=llm_config,
        )
        return assistant, user_proxy

    def _extract_text(self, parts: list[Part]) -> str:
        texts = [p.text for p in parts if p.type == "text" and p.text]
        return "\n".join(texts) if texts else ""

    def _last_assistant_text(self, chat_result: Any) -> str:
        """Pull the final assistant message from a chat_result."""
        try:
            messages = getattr(chat_result, "chat_history", [])
            for msg in reversed(messages):
                role = getattr(msg, "role", "") or msg.get("role", "")
                if "assistant" in str(role):
                    content = getattr(msg, "content", "") or msg.get("content", "")
                    return str(content)
            return str(messages[-1]) if messages else ""
        except Exception:
            return str(chat_result)

    # -- Invocation --------------------------------------------------------

    async def invoke(self, req: InvokeRequest) -> InvokeResponse:
        self._start_time = time.time()
        task_id = req.task_id or "autogen-invoke"
        t0 = time.time()

        try:
            assistant, user_proxy = self._build_agents()
            message = self._extract_text(req.messages)
            if not message:
                message = "Hello."

            loop = asyncio.get_event_loop()

            def _chat() -> Any:
                return user_proxy.initiate_chat(
                    assistant, message=message, max_turns=2
                )

            chat_result = await loop.run_in_executor(None, _chat)
            response_text = self._last_assistant_text(chat_result)

            return InvokeResponse(
                task_id=task_id,
                status="completed",
                messages=[Part(type="text", text=response_text)],
                tokens_used=len(response_text.split()),
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
        task_id = req.task_id or "autogen-stream"
        self._active_tasks[task_id] = False

        yield StreamEvent(task_id=task_id, event_type="status", status="started")

        try:
            assistant, user_proxy = self._build_agents()
            message = self._extract_text(req.messages) or "Hello."

            loop = asyncio.get_event_loop()
            chat_result = await loop.run_in_executor(
                None,
                lambda: user_proxy.initiate_chat(assistant, message=message, max_turns=2),
            )
            response_text = self._last_assistant_text(chat_result)

            yield StreamEvent(
                task_id=task_id,
                event_type="delta",
                delta=Part(type="text", text=response_text),
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
