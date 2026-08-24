"""
Shared request builders for the adapter REST API.

Contains small helpers used by multiple router endpoints in ``api``.
"""

from __future__ import annotations

from oap.adapters.types import InvokeRequest


def build_invoke_request(task_id: str, body) -> InvokeRequest:
    """Lift an API request-body model into a shared InvokeRequest.

    The body's ``adapter_name`` is encoded as the ``preferred_agent``
    metadata entry that the orchestrator uses for routing.
    """
    metadata = dict(body.metadata)
    if body.adapter_name:
        metadata["preferred_agent"] = body.adapter_name

    return InvokeRequest(
        task_id=task_id,
        messages=body.messages,
        metadata=metadata,
        timeout_seconds=body.timeout,
    )
