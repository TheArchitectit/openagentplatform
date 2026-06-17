"""
Shared types for the OAP adapter subsystem.

These types form the contract between the adapter layer and the rest of the
platform (A2A gateway, orchestration engine, cost manager, etc.). They are
intentionally framework-agnostic — concrete adapters translate between these
types and framework-native objects.
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field

# ---------------------------------------------------------------------------
# Part — a single content unit in a message (text, file, or data).
# ---------------------------------------------------------------------------


class Part(BaseModel):
    """A single content unit carried in messages and artifacts.

    Attributes:
        type: The kind of content ("text", "file", or "data").
        text: Text content (populated when type == "text").
        file_url: URL or path to a file (populated when type == "file").
        file_mime: MIME type of the file (populated when type == "file").
        data: Arbitrary structured data (populated when type == "data").
    """

    type: Literal["text", "file", "data"] = "text"
    text: str = ""
    file_url: str = ""
    file_mime: str = ""
    data: dict[str, Any] = Field(default_factory=dict)


# ---------------------------------------------------------------------------
# AgentSkill — a single discoverable capability exposed by an agent.
# ---------------------------------------------------------------------------


class AgentSkill(BaseModel):
    """A discoverable capability that an agent exposes for routing.

    Attributes:
        name: Short identifier for the skill.
        description: Human-readable explanation of what the skill does.
        tags: Free-form tags used for keyword-based skill matching.
        input_schema: JSON Schema describing the expected input.
        output_schema: JSON Schema describing the produced output.
    """

    name: str
    description: str = ""
    tags: list[str] = Field(default_factory=list)
    input_schema: dict[str, Any] = Field(default_factory=dict)
    output_schema: dict[str, Any] = Field(default_factory=dict)


# ---------------------------------------------------------------------------
# AgentCard — static discovery metadata for an agent.
# ---------------------------------------------------------------------------


class AgentCard(BaseModel):
    """Static discovery metadata describing an agent endpoint.

    Returned by `AgentWrapper.agent_card` and used by the A2A gateway for
    skill-based routing and capability discovery.

    Attributes:
        name: Agent name (must be unique within the platform).
        description: Human-readable description.
        version: Semantic version string of the agent implementation.
        url: Endpoint URL where the agent can be reached.
        provider_name: Organization that built/maintains the agent.
        provider_url: URL of the provider organization.
        skills: List of skills the agent exposes.
        streaming: Whether the agent supports streaming responses.
        push_notifications: Whether the agent supports push notifications.
        auth_schemes: Supported authentication schemes.
        default_input_modes: Default content types accepted as input.
        default_output_modes: Default content types produced as output.
    """

    name: str
    description: str = ""
    version: str = "0.0.0"
    url: str = ""
    provider_name: str = ""
    provider_url: str = ""
    skills: list[AgentSkill] = Field(default_factory=list)
    streaming: bool = False
    push_notifications: bool = False
    auth_schemes: list[str] = Field(default_factory=list)
    default_input_modes: list[str] = Field(default_factory=lambda: ["text"])
    default_output_modes: list[str] = Field(default_factory=lambda: ["text"])


# ---------------------------------------------------------------------------
# InvokeRequest / InvokeResponse — request/response interaction types.
# ---------------------------------------------------------------------------


class InvokeRequest(BaseModel):
    """A single request/response invocation request.

    Attributes:
        task_id: Caller-supplied or platform-generated task identifier.
        messages: Ordered list of message Parts (text/file/data).
        metadata: Arbitrary caller-supplied metadata.
        timeout_seconds: Maximum wall-clock seconds before the invocation
            is forcibly terminated.
    """

    task_id: str = ""
    messages: list[Part] = Field(default_factory=list)
    metadata: dict[str, Any] = Field(default_factory=dict)
    timeout_seconds: float = 120.0


class InvokeResponse(BaseModel):
    """The result of a request/response invocation.

    Attributes:
        task_id: The task identifier from the corresponding InvokeRequest.
        status: Terminal status string (e.g., "completed", "failed",
            "input_required", "canceled").
        messages: Response message Parts produced by the agent.
        artifacts: Structured artifacts (file references, data payloads).
        cost: Optional cost record for this invocation.
        tokens_used: Total tokens consumed (prompt + completion).
        duration_ms: Wall-clock duration of the invocation in milliseconds.
        error_message: Human-readable error description (populated on failure).
    """

    task_id: str = ""
    status: str = "completed"
    messages: list[Part] = Field(default_factory=list)
    artifacts: list[dict[str, Any]] = Field(default_factory=list)
    cost: CostRecord | None = None  # type: ignore[assignment]
    tokens_used: int = 0
    duration_ms: int = 0
    error_message: str = ""


# ---------------------------------------------------------------------------
# StreamEvent — a single event in a streaming response.
# ---------------------------------------------------------------------------


class StreamEvent(BaseModel):
    """A single event yielded by an adapter's `stream()` method.

    Attributes:
        task_id: The task identifier this event belongs to.
        event_type: Event kind — "delta" (incremental content), "status"
            (state update), "error" (stream-level error), or "done"
            (terminal marker).
        delta: Content delta carried by this event (populated for
            event_type == "delta").
        status: Status string (populated for event_type == "status").
        metadata: Arbitrary event metadata.
    """

    task_id: str = ""
    event_type: Literal["delta", "status", "error", "done"] = "delta"
    delta: Part | None = None
    status: str = ""
    metadata: dict[str, Any] = Field(default_factory=dict)


# ---------------------------------------------------------------------------
# HealthStatus — adapter liveness and resource snapshot.
# ---------------------------------------------------------------------------


class HealthStatus(BaseModel):
    """Snapshot of an adapter's health and resource usage.

    Attributes:
        healthy: True if the adapter is operational.
        last_error: Description of the last error (empty when healthy).
        uptime_seconds: Seconds since the adapter was started.
        active_tasks: Number of currently in-flight tasks.
        memory_mb: Approximate resident memory usage in megabytes.
    """

    healthy: bool = True
    last_error: str = ""
    uptime_seconds: float = 0.0
    active_tasks: int = 0
    memory_mb: float = 0.0


# ---------------------------------------------------------------------------
# CostRecord — token usage and cost for a single task.
# ---------------------------------------------------------------------------


class CostRecord(BaseModel):
    """Token usage and monetary cost for a single task.

    Attributes:
        task_id: The task this record belongs to.
        framework: Name of the framework adapter that produced this record.
        model: Model identifier (e.g., "claude-opus-4-20250514").
        prompt_tokens: Tokens consumed by the prompt/input.
        completion_tokens: Tokens generated by the model.
        total_cost: Total cost in the specified currency.
        currency: ISO 4217 currency code (e.g., "USD").
    """

    task_id: str = ""
    framework: str = ""
    model: str = ""
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_cost: float = 0.0
    currency: str = "USD"
