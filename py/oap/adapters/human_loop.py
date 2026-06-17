"""
Human-in-the-Loop approval gate for the OAP adapter subsystem.

Provides an approval workflow that pauses task execution pending human
authorisation. Supports automatic approval for low-cost actions, webhook
integration for external approval systems, and A2A-compatible state
management (input-required maps to approval pending).
"""

from __future__ import annotations

import asyncio
import enum
import time
import uuid
from typing import Any, Awaitable, Callable

from pydantic import BaseModel, Field

# ---------------------------------------------------------------------------
# Approval status enum.
# ---------------------------------------------------------------------------


class ApprovalStatus(str, enum.Enum):
    """Lifecycle states for an ApprovalRequest."""

    PENDING = "pending"
    APPROVED = "approved"
    REJECTED = "rejected"
    TIMED_OUT = "timed_out"
    AUTO_APPROVED = "auto_approved"


# ---------------------------------------------------------------------------
# ApprovalRequest — a single approval gate.
# ---------------------------------------------------------------------------


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


# ---------------------------------------------------------------------------
# Webhook delivery function type.
# ---------------------------------------------------------------------------

WebhookFn = Callable[[ApprovalRequest], Awaitable[None]]

# ---------------------------------------------------------------------------
# A2A state mapping constant.
# ---------------------------------------------------------------------------

# In the A2A protocol, the task state "input-required" means the agent is
# waiting for human input. We use this to signal that an approval is pending.
A2A_INPUT_REQUIRED = "input-required"

# Default auto-approval threshold in USD.
DEFAULT_AUTO_APPROVE_LIMIT: float = 50.0


# ---------------------------------------------------------------------------
# ApprovalGate — the main human-in-the-loop controller.
# ---------------------------------------------------------------------------


class ApprovalGate:
    """Manages approval requests for adapter actions requiring human authorisation.

    Provides:
    - Explicit approval creation and waiting (create_approval / wait_for_approval).
    - Automatic approval for actions below a configurable cost threshold.
    - Webhook delivery to external approval systems.
    - A2A-compatible state mapping (pending -> input-required).
    - Timeout handling with configurable default.

    Attributes:
        _pending: In-flight approval requests awaiting resolution.
        _resolved: Completed approval requests (all terminal states).
        _webhook: Optional webhook function for external notifications.
        _auto_approve_limit: Cost threshold below which actions are auto-approved.
        _default_timeout: Default timeout in seconds for new approval requests.
    """

    def __init__(
        self,
        webhook: WebhookFn | None = None,
        auto_approve_limit: float = DEFAULT_AUTO_APPROVE_LIMIT,
        default_timeout: float = 300.0,
    ) -> None:
        self._pending: dict[str, ApprovalRequest] = {}
        self._resolved: dict[str, ApprovalRequest] = {}
        self._webhook: WebhookFn | None = webhook
        self._auto_approve_limit: float = auto_approve_limit
        self._default_timeout: float = default_timeout
        self._events: dict[str, asyncio.Event] = {}

    # -- Approval creation --------------------------------------------------

    async def create_approval(
        self,
        task_id: str,
        adapter: str,
        action: str,
        details: dict[str, Any] | None = None,
        options: list[str] | None = None,
        estimated_cost: float = 0.0,
        timeout_seconds: float | None = None,
    ) -> ApprovalRequest:
        """Create a new approval request and return it.

        If the estimated cost is below the auto-approve limit, the request
        is immediately resolved as AUTO_APPROVED without waiting.

        Args:
            task_id: The task requiring approval.
            adapter: The framework adapter name.
            action: Description of the action to approve.
            details: Additional context about the action.
            options: Acceptable response options.
            estimated_cost: Estimated cost of the action in USD.
            timeout_seconds: Timeout override (uses default if None).

        Returns:
            The created ApprovalRequest (may already be resolved).
        """
        req = ApprovalRequest(
            task_id=task_id,
            adapter=adapter,
            action=action,
            details=details or {},
            options=options or ["approve", "reject"],
            estimated_cost=estimated_cost,
            timeout_seconds=(
                timeout_seconds if timeout_seconds is not None else self._default_timeout
            ),
        )

        # Auto-approve if cost is within the limit.
        if estimated_cost <= self._auto_approve_limit:
            req.status = ApprovalStatus.AUTO_APPROVED
            req.resolved_at = time.time()
            req.resolved_by = "system:auto-approve"
            self._resolved[req.approval_id] = req
            return req

        # Otherwise, register for human review.
        self._pending[req.approval_id] = req
        self._events[req.approval_id] = asyncio.Event()

        # Fire webhook for external approval systems.
        if self._webhook is not None:
            try:
                await self._webhook(req)
            except Exception:
                pass  # Webhook failures should not block the approval flow.

        return req

    # -- Waiting for approval -----------------------------------------------

    async def wait_for_approval(
        self,
        approval_id: str,
        timeout_seconds: float | None = None,
    ) -> ApprovalRequest:
        """Wait for a human to resolve an approval request.

        Blocks until the request is resolved (approved, rejected, timed out)
        or the timeout expires. If the timeout expires, the request is
        automatically rejected.

        Args:
            approval_id: The approval request to wait on.
            timeout_seconds: Override timeout (uses request's own if None).

        Returns:
            The resolved ApprovalRequest with its final status.
        """
        req = self._pending.get(approval_id)
        if req is None:
            # Check if it was already resolved.
            resolved = self._resolved.get(approval_id)
            if resolved is not None:
                return resolved
            raise KeyError(f"No approval request found with id: {approval_id}")

        effective_timeout = timeout_seconds if timeout_seconds is not None else req.timeout_seconds
        event = self._events.get(approval_id)
        if event is None:
            event = asyncio.Event()
            self._events[approval_id] = event

        try:
            await asyncio.wait_for(event.wait(), timeout=effective_timeout)
        except asyncio.TimeoutError:
            # Timeout — mark as rejected.
            req.status = ApprovalStatus.TIMED_OUT
            req.resolved_at = time.time()
            req.resolved_by = "system:timeout"
            self._pending.pop(approval_id, None)
            self._resolved[approval_id] = req
            event.set()

        return req

    # -- Manual resolution --------------------------------------------------

    def resolve(
        self,
        approval_id: str,
        approved: bool,
        resolved_by: str = "",
    ) -> ApprovalRequest:
        """Manually resolve a pending approval request.

        Args:
            approval_id: The approval request to resolve.
            approved: True to approve, False to reject.
            resolved_by: Identifier of the person/system resolving.

        Returns:
            The resolved ApprovalRequest.

        Raises:
            KeyError: If the approval_id is not found in pending requests.
        """
        req = self._pending.pop(approval_id, None)
        if req is None:
            raise KeyError(
                f"No pending approval request found with id: {approval_id}"
            )

        req.status = ApprovalStatus.APPROVED if approved else ApprovalStatus.REJECTED
        req.resolved_at = time.time()
        req.resolved_by = resolved_by
        self._resolved[approval_id] = req

        event = self._events.pop(approval_id, None)
        if event is not None:
            event.set()

        return req

    # -- Queries ------------------------------------------------------------

    def get_approval(self, approval_id: str) -> ApprovalRequest | None:
        """Look up an approval request by ID (pending or resolved).

        Args:
            approval_id: The approval request identifier.

        Returns:
            The ApprovalRequest, or None if not found.
        """
        return self._pending.get(approval_id) or self._resolved.get(approval_id)

    def get_pending(self) -> list[ApprovalRequest]:
        """Return all currently pending approval requests."""
        return list(self._pending.values())

    def get_resolved(self) -> list[ApprovalRequest]:
        """Return all resolved approval requests."""
        return list(self._resolved.values())

    # -- A2A state integration ----------------------------------------------

    def get_a2a_state(self, approval_id: str) -> str:
        """Map an approval request to an A2A task state string.

        A pending approval maps to "input-required". Resolved approvals
        return their terminal status string.

        Args:
            approval_id: The approval request identifier.

        Returns:
            The A2A-compatible state string.
        """
        req = self.get_approval(approval_id)
        if req is None:
            return "unknown"
        if req.status == ApprovalStatus.PENDING:
            return A2A_INPUT_REQUIRED
        return req.status.value

    # -- Webhook configuration ----------------------------------------------

    def set_webhook(self, webhook: WebhookFn | None) -> None:
        """Set or replace the webhook function for external notifications.

        Args:
            webhook: An async callable that receives ApprovalRequest,
                or None to disable webhooks.
        """
        self._webhook = webhook

    # -- Configuration ------------------------------------------------------

    @property
    def auto_approve_limit(self) -> float:
        """The current auto-approval cost threshold in USD."""
        return self._auto_approve_limit

    @auto_approve_limit.setter
    def auto_approve_limit(self, value: float) -> None:
        self._auto_approve_limit = value
