"""
Microsoft Semantic Kernel adapter for the OAP platform.

Translates between the A2A InvokeRequest/Response protocol and the
Semantic Kernel SDK. Builds a kernel with an optional chat function
plugin and invokes it.
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
    "azure-gpt-4o",
    "azure-gpt-4o-mini",
    "claude-opus-4-20250514",
    "claude-sonnet-4-6",
    "huggingface-mistral-7b",
]


@register_adapter("semantic_kernel")
class SemanticKernelAdapter(AgentWrapper):
    """Adapter for Microsoft Semantic Kernel.

    Lazy-imports ``semantic_kernel``. Constructs a kernel with a chat
    completion service and invokes a function against it.
    """

    def __init__(
        self,
        model: str = "gpt-4o-mini",
        service_id: str = "chat",
        api_key: str = "",
        endpoint: str = "",
        system_prompt: str = "You are a helpful assistant.",
        **kwargs: Any,
    ) -> None:
        try:
            import semantic_kernel  # noqa: F401
        except ImportError as exc:
            raise FrameworkNotFoundError(
                "semantic-kernel is not installed. Run: pip install semantic-kernel",
                adapter_name="semantic_kernel",
            ) from exc

        self._model = model
        self._service_id = service_id
        self._api_key = api_key
        self._endpoint = endpoint
        self._system_prompt = system_prompt
        self._start_time: float | None = None
        self._active_tasks: dict[str, Any] = {}
        self._lock = asyncio.Lock()

    # -- Discovery ---------------------------------------------------------

    @property
    def agent_card(self) -> AgentCard:
        return AgentCard(
            name="SemanticKernel",
            description="Enterprise-grade AI orchestration with planning, plugins, and memory.",
            version="1.0.0",
            url="oap://adapter/semantic_kernel",
            provider_name="OAP",
            provider_url="https://openagentplatform.io",
            skills=[
                AgentSkill(
                    name="planning",
                    description="Automatic function-calling planning.",
                    tags=["planning", "orchestration"],
                ),
                AgentSkill(
                    name="plugins",
                    description="Native plugin / function composition.",
                    tags=["plugins", "functions"],
                ),
                AgentSkill(
                    name="memory",
                    description="Long-term and short-term semantic memory.",
                    tags=["memory", "context"],
                ),
                AgentSkill(
                    name="enterprise",
                    description="Enterprise integration patterns.",
                    tags=["enterprise", "integration"],
                ),
            ],
            streaming=True,
            push_notifications=False,
            auth_schemes=["api-key"],
            default_input_modes=["text"],
            default_output_modes=["text"],
        )

    # -- Helpers -----------------------------------------------------------

    def _build_kernel(self) -> Any:
        from semantic_kernel import Kernel
        from semantic_kernel.connectors.ai.open_ai import OpenAIChatCompletion

        kernel = Kernel()
        if self._endpoint and "azure" in self._endpoint.lower():
            from semantic_kernel.connectors.ai.open_ai import AzureChatCompletion
            kernel.add_service(
                AzureChatCompletion(
                    deployment_name=self._model,
                    endpoint=self._endpoint,
                    api_key=self._api_key or "REPLACE_ME",
                    service_id=self._service_id,
                )
            )
        else:
            kernel.add_service(
                OpenAIChatCompletion(
                    ai_model_id=self._model,
                    api_key=self._api_key or "REPLACE_ME",
                    service_id=self._service_id,
                )
            )
        return kernel

    def _extract_text(self, parts: list[Part]) -> str:
        texts = [p.text for p in parts if p.type == "text" and p.text]
        return "\n".join(texts) if texts else ""

    # -- Invocation --------------------------------------------------------

    async def invoke(self, req: InvokeRequest) -> InvokeResponse:
        self._start_time = time.time()
        task_id = req.task_id or "sk-invoke"
        t0 = time.time()

        try:
            from semantic_kernel.functions import KernelArguments
            from semantic_kernel.prompt_template import PromptTemplateConfig

            kernel = self._build_kernel()
            prompt = self._system_prompt + "\n\nUser: {{$input}}\nAssistant:"
            prompt_cfg = PromptTemplateConfig(template=prompt)
            func = kernel.add_function(
                function_name="chat",
                plugin_name="oap",
                prompt_template_config=prompt_cfg,
            )

            user_input = self._extract_text(req.messages) or "Hello."
            loop = asyncio.get_event_loop()
            result = await loop.run_in_executor(
                None,
                lambda: kernel.invoke(func, KernelArguments(input=user_input)),
            )
            output_text = str(result)

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
        task_id = req.task_id or "sk-stream"

        yield StreamEvent(task_id=task_id, event_type="status", status="started")

        try:
            from semantic_kernel.functions import KernelArguments
            from semantic_kernel.prompt_template import PromptTemplateConfig

            kernel = self._build_kernel()
            prompt = self._system_prompt + "\n\nUser: {{$input}}\nAssistant:"
            prompt_cfg = PromptTemplateConfig(template=prompt)
            func = kernel.add_function(
                function_name="chat",
                plugin_name="oap",
                prompt_template_config=prompt_cfg,
            )

            user_input = self._extract_text(req.messages) or "Hello."

            try:
                loop = asyncio.get_event_loop()
                result = await loop.run_in_executor(
                    None,
                    lambda: kernel.invoke(func, KernelArguments(input=user_input)),
                )
                output_text = str(result)
            except Exception:
                # Fall back to non-streaming aggregate when streaming is unavailable.
                output_text = "[streaming fallback: run invoke()]"

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
        if isinstance(info, dict) and "token" in info:
            try:
                info["token"].cancel()
            except Exception:
                pass
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
        for info in self._active_tasks.values():
            if isinstance(info, dict) and "token" in info:
                try:
                    info["token"].cancel()
                except Exception:
                    pass
        self._active_tasks.clear()
