"""
Data models and constants for the human-in-the-loop approval subsystem.

Holds the approval status enum, the ApprovalRequest model, the webhook
signature, and the default auto-approval threshold used by
:class:`oap.adapters.human_loop.ApprovalGate`.
"""

from __future__ import annotations

import time
import uuid
from collections.abc import Awaitable, Callable
from enum import StrEnum
from typing import Any

from pydantic import BaseModel, Field


class ApprovalStatus(StrEnum):
    """Lifecycle states for an ApprovalRequest."""

    PENDING = "pending"
    APPROVED = "approved"
    REJECTED = "rejected"
    TIMED_OUT = "timed_out"
    AUTO_APPROVED = "auto_approved"


class ApprovalRequest(BaseModel):
    """A request for human approval before proceeding with a task action.

    Attributes:
        approval_id: Unique identifier for this approval request.
        task_id: The task this approval is gating.
        adapter: The framework adapter requesting approval.
        action: Description of the action that requires approval.
        details: Additional context about the action.
        options: Acceptable response options (e.g., ["approve", "reject"]).
        estimated_cost: Estimated cost of the action in USD.
        status: Current approval status.
        timeout_seconds: Maximum seconds to wait before auto-action.
        created_at: Unix epoch time when the request was created.
        resolved_at: Unix epoch time when the request was resolved.
        resolved_by: Identifier of the approver (human or system).
    """

    approval_id: str = Field(default_factory=lambda: str(uuid.uuid4()))
    task_id: str
    adapter: str
    action: str
    details: dict[str, Any] = Field(default_factory=dict)
    options: list[str] = Field(default_factory=lambda: ["approve", "reject"])
    estimated_cost: float = 0.0
    status: ApprovalStatus = ApprovalStatus.PENDING
    timeout_seconds: float = 300.0
    created_at: float = Field(default_factory=time.time)
    resolved_at: float = 0.0
    resolved_by: str = ""


# Webhook delivery function type.
WebhookFn = Callable[[ApprovalRequest], Awaitable[None]]

# In the A2A protocol, the task state "input-required" means the agent is
# waiting for human input. We use this to signal that an approval is pending.
A2A_INPUT_REQUIRED = "input-required"

# Default auto-approval threshold in USD.
DEFAULT_AUTO_APPROVE_LIMIT: float = 50.0
