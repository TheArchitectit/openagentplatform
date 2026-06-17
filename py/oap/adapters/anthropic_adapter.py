"""
Anthropic Claude adapter for the OAP platform.

Translates between the A2A InvokeRequest/Response protocol and the
official Anthropic Python SDK (``anthropic.Anthropic``). Supports
invoke with tool definitions and streaming via message events.
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
    "claude-opus-4-20250514",
    "claude-sonnet-4-6",
    "claude-haiku-4-5",
]


@register_adapter("anthropic")
class AnthropicAdapter(AgentWrapper):
    """Adapter for the Anthropic Claude SDK.

    Lazy-imports ``anthropic``. Uses ``client.messages.create`` for
    invoke and ``client.messages.stream`` for streaming.
    """

    def __init__(
        self,
        model: str = "claude-sonnet-4-6",
        max_tokens: int = 4096,
        system: str = "You are a helpful assistant.",
        api_key: str = "",
        tools: list[dict] | None = None,
        **kwargs: Any,
    ) -> None:
        try:
            from anthropic import Anthropic  # noqa: F401
        except ImportError as exc:
            raise FrameworkNotFoundError(
                "anthropic SDK is not installed. Run: pip install anthropic",
                adapter_name="anthropic",
            ) from exc

        self._model = model
        self._max_tokens = max_tokens
        self._system = system
        self._api_key = api_key
        self._tools = tools or []
        self._start_time: float | None = None
        self._active_tasks: dict[str, bool] = {}
        self._lock = asyncio.Lock()

    # -- Discovery ---------------------------------------------------------

    @property
    def agent_card(self) -> AgentCard:
        return AgentCard(
            name="Claude",
            description="Anthropic Claude models with tool use, vision, and long-context reasoning.",
            version="1.0.0",
            url="oap://adapter/anthropic",
            provider_name="OAP",
            provider_url="https://openagentplatform.io",
            skills=[
                AgentSkill(
                    name="reasoning",
                    description="Advanced chain-of-thought reasoning.",
                    tags=["reasoning", "claude"],
                ),
                AgentSkill(
                    name="tool-use",
                    description="Native tool / function calling.",
                    tags=["tools", "function-calling"],
                ),
                AgentSkill(
                    name="analysis",
                    description="Deep analysis of complex documents and data.",
                    tags=["analysis", "documents"],
                ),
                AgentSkill(
                    name="coding",
                    description="Software engineering and code generation.",
                    tags=["coding", "engineering"],
                ),
                AgentSkill(
                    name="vision",
                    description="Image and visual content understanding.",
                    tags=["vision", "multimodal"],
                ),
            ],
            streaming=True,
            push_notifications=False,
            auth_schemes=["api-key"],
            default_input_modes=["text", "image"],
            default_output_modes=["text"],
        )

    # -- Helpers -----------------------------------------------------------

    def _get_client(self) -> Any:
        from anthropic import Anthropic
        return Anthropic(api_key=self._api_key or "REPLACE_ME")

    def _build_messages(self, parts: list[Part]) -> list[dict]:
        """Translate OAP Parts into Anthropic message content blocks."""
        content: list[dict] = []
        for p in parts:
            if p.type == "text" and p.text:
                content.append({"type": "text", "text": p.text})
            elif p.type == "file" and p.file_url:
                content.append({
                    "type": "image",
                    "source": {"type": "url", "url": p.file_url},
                })
        if not content:
            content = [{"type": "text", "text": "Hello."}]
        return [{"role": "user", "content": content}]

    def _usage_tokens(self, response: Any) -> int:
        try:
            return int(getattr(response.usage, "input_tokens", 0)) + int(
                getattr(response.usage, "output_tokens", 0)
            )
        except Exception:
            return 0

    # -- Invocation --------------------------------------------------------

    async def invoke(self, req: InvokeRequest) -> InvokeResponse:
        self._start_time = time.time()
        task_id = req.task_id or "anthropic-invoke"
        t0 = time.time()

        try:
            client = self._get_client()
            messages = self._build_messages(req.messages)
            kwargs: dict = {
                "model": self._model,
                "max_tokens": self._max_tokens,
                "system": self._system,
                "messages": messages,
            }
            if self._tools:
                kwargs["tools"] = self._tools

            loop = asyncio.get_event_loop()
            response = await loop.run_in_executor(
                None, lambda: client.messages.create(**kwargs)
            )

            text_parts: list[str] = []
            for block in getattr(response, "content", []):
                if getattr(block, "type", "") == "text":
                    text_parts.append(getattr(block, "text", ""))
            output_text = "".join(text_parts) or "[empty response]"

            return InvokeResponse(
                task_id=task_id,
                status="completed",
                messages=[Part(type="text", text=output_text)],
                tokens_used=self._usage_tokens(response),
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
        task_id = req.task_id or "anthropic-stream"
        self._active_tasks[task_id] = False

        yield StreamEvent(task_id=task_id, event_type="status", status="started")

        try:
            client = self._get_client()
            messages = self._build_messages(req.messages)
            kwargs: dict = {
                "model": self._model,
                "max_tokens": self._max_tokens,
                "system": self._system,
                "messages": messages,
            }
            if self._tools:
                kwargs["tools"] = self._tools

            loop = asyncio.get_event_loop()

            def _collect() -> str:
                chunks: list[str] = []
                with client.messages.stream(**kwargs) as stream:
                    for event in stream:
                        if self._active_tasks.get(task_id):
                            break
                        if getattr(event, "type", "") == "content_block_delta":
                            delta = getattr(event, "delta", None)
                            if delta and getattr(delta, "text", None):
                                chunks.append(delta.text)
                return "".join(chunks)

            output_text = await loop.run_in_executor(None, _collect)

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
        for task_id in list(self._active_tasks.keys()):
            self._active_tasks[task_id] = True
        self._active_tasks.clear()
