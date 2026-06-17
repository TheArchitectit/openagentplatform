"""
Ozore AI adapter — OpenAI-compatible hosted LLM agent provider.

Uses the standard OpenAI Python client pointed at the Ozore API endpoint.
The adapter is registered as "ozore" and can be used for any A2A task
that requires an LLM agent (reasoning, tool-use, coding, analysis).

API key is read from OZORE_API_KEY env var (never hardcoded).
"""

from __future__ import annotations

import asyncio
import os
import time
from typing import AsyncIterator, Optional

from oap.adapters.errors import AdapterError, FrameworkNotFoundError, InvocationError, TimeoutError
from oap.adapters.types import (
    AgentCard,
    AgentSkill,
    CostRecord,
    HealthStatus,
    InvokeRequest,
    InvokeResponse,
    Part,
    StreamEvent,
)
from oap.adapters.wrapper import AgentWrapper, register_adapter

OZORE_API_KEY = os.environ.get("OZORE_API_KEY", "")
OZORE_MODEL = os.environ.get("OZORE_MODEL", "ozore/custom")
OZORE_BASE_URL = os.environ.get("OZORE_BASE_URL", "https://ozore.com/v1")


@register_adapter("ozore")
class OzoreAdapter(AgentWrapper):
    """OpenAI-compatible adapter for the Ozore hosted agent platform."""

    HEALTHY_COST_MODELS: list[str] = [OZORE_MODEL]

    def __init__(self, api_key: Optional[str] = None, model: Optional[str] = None, base_url: Optional[str] = None):
        self._api_key = api_key or OZORE_API_KEY
        self._model = model or OZORE_MODEL
        self._base_url = base_url or OZORE_BASE_URL
        self._client = None
        self._active_tasks: dict[str, asyncio.Task] = {}
        self._started_at: Optional[float] = None
        self._healthy = True
        self._last_error: Optional[str] = None

    # ------------------------------------------------------------------
    # Lazy import
    # ------------------------------------------------------------------
    @staticmethod
    def _get_openai():
        try:
            from openai import AsyncOpenAI  # type: ignore[import-untyped]
            return AsyncOpenAI
        except ImportError:
            raise FrameworkNotFoundError(
                "openai package is required for Ozore adapter.  Install with: pip install openai"
            ) from None

    # ------------------------------------------------------------------
    # AgentCard
    # ------------------------------------------------------------------
    @property
    def agent_card(self) -> AgentCard:
        return AgentCard(
            name="Ozore",
            description=f"Hosted LLM agent — OpenAI-compatible API (model: {self._model})",
            version="1.0.0",
            url=self._base_url,
            provider_name="Ozore",
            provider_url="https://ozore.com",
            skills=[
                AgentSkill(
                    name="reasoning",
                    description="General-purpose reasoning and analysis",
                    tags=["reasoning", "analysis", "problem-solving"],
                ),
                AgentSkill(
                    name="tool-use",
                    description="Function/tool calling with structured outputs",
                    tags=["tool-use", "function-calling", "structured-output"],
                ),
                AgentSkill(
                    name="coding",
                    description="Code generation, review, and debugging",
                    tags=["coding", "code-review", "debugging"],
                ),
                AgentSkill(
                    name="vision",
                    description="Image understanding (if model supports it)",
                    tags=["vision", "image-analysis"],
                ),
            ],
            streaming=True,
            push_notifications=False,
            auth_schemes=[],
            default_input_modes=["text"],
            default_output_modes=["text"],
        )

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------
    async def start(self) -> None:
        if not self._api_key:
            raise AdapterError("OZORE_API_KEY is required.  Set it in .env or the environment.")

        client_cls = self._get_openai()
        self._client = client_cls(api_key=self._api_key, base_url=self._base_url)
        self._started_at = time.monotonic()
        self._healthy = True
        self._last_error = None

    async def stop(self) -> None:
        for task_id, task in list(self._active_tasks.items()):
            task.cancel()
        self._active_tasks.clear()
        if self._client is not None:
            await self._client.close()
            self._client = None
        self._started_at = None

    # ------------------------------------------------------------------
    # invoke (non-streaming)
    # ------------------------------------------------------------------
    async def invoke(self, req: InvokeRequest) -> InvokeResponse:
        if self._client is None:
            raise AdapterError("Ozore adapter not started")

        started = time.monotonic()
        timeout = req.timeout_seconds or 60

        messages = self._parts_to_openai_messages(req.messages)
        try:
            completion = await asyncio.wait_for(
                self._client.chat.completions.create(
                    model=self._model,
                    messages=messages,
                    temperature=0.7,
                    max_tokens=4096,
                ),
                timeout=timeout,
            )
        except asyncio.TimeoutError:
            self._last_error = f"invoke timed out after {timeout}s"
            raise TimeoutError(self._last_error)
        except Exception as exc:
            self._last_error = str(exc)
            self._healthy = False
            raise InvocationError(str(exc)) from exc

        elapsed = time.monotonic() - started
        self._healthy = True

        choice = completion.choices[0]
        response_text = choice.message.content or ""

        prompt_tokens = completion.usage.prompt_tokens if completion.usage else 0
        completion_tokens = completion.usage.completion_tokens if completion.usage else 0

        cost = CostRecord(
            task_id=req.task_id,
            framework="ozore",
            model=self._model,
            prompt_tokens=prompt_tokens,
            completion_tokens=completion_tokens,
            total_cost=0.0,  # Ozore pricing is account-specific
            currency="USD",
        )

        return InvokeResponse(
            task_id=req.task_id,
            status="completed",
            messages=[Part(type="text", text=response_text)],
            cost=cost,
            tokens_used=prompt_tokens + completion_tokens,
            duration_ms=int(elapsed * 1000),
        )

    # ------------------------------------------------------------------
    # stream
    # ------------------------------------------------------------------
    async def stream(self, req: InvokeRequest) -> AsyncIterator[StreamEvent]:
        if self._client is None:
            raise AdapterError("Ozore adapter not started")

        timeout = req.timeout_seconds or 120

        messages = self._parts_to_openai_messages(req.messages)
        try:
            stream = await asyncio.wait_for(
                self._client.chat.completions.create(
                    model=self._model,
                    messages=messages,
                    temperature=0.7,
                    max_tokens=4096,
                    stream=True,
                ),
                timeout=timeout if timeout > 0 else None,
            )
        except asyncio.TimeoutError:
            yield StreamEvent(task_id=req.task_id, event_type="error", error_message="stream timed out")
            return
        except Exception as exc:
            yield StreamEvent(task_id=req.task_id, event_type="error", error_message=str(exc))
            return

        try:
            async for chunk in stream:
                delta = chunk.choices[0].delta if chunk.choices else None
                if delta and delta.content:
                    yield StreamEvent(
                        task_id=req.task_id,
                        event_type="delta",
                        delta=Part(type="text", text=delta.content),
                    )
        except Exception as exc:
            yield StreamEvent(task_id=req.task_id, event_type="error", error_message=str(exc))
            return

        yield StreamEvent(task_id=req.task_id, event_type="done")

    # ------------------------------------------------------------------
    # cancel
    # ------------------------------------------------------------------
    async def cancel(self, task_id: str) -> bool:
        t = self._active_tasks.pop(task_id, None)
        if t is not None:
            t.cancel()
            return True
        return False

    # ------------------------------------------------------------------
    # health
    # ------------------------------------------------------------------
    async def health(self) -> HealthStatus:
        uptime = (time.monotonic() - self._started_at) if self._started_at else 0.0
        return HealthStatus(
            healthy=self._healthy,
            last_error=self._last_error,
            uptime_seconds=uptime,
            active_tasks=len(self._active_tasks),
            memory_mb=0.0,  # Not tracked for external service
        )

    # ------------------------------------------------------------------
    # helpers
    # ------------------------------------------------------------------
    @staticmethod
    def _parts_to_openai_messages(parts: list[Part]) -> list[dict]:
        """Convert A2A Parts to OpenAI message format."""
        messages: list[dict] = []
        for p in parts:
            if p.type == "text":
                messages.append({"role": "user", "content": p.text})
            elif p.type == "file" and p.file_url:
                messages.append({"role": "user", "content": f"[File: {p.file_url}]"})
            elif p.type == "data":
                messages.append({"role": "user", "content": str(p.data)})
        if not messages:
            messages.append({"role": "user", "content": "Please assist."})
        return messages
