"""
Exception taxonomy for the OAP adapter subsystem.

All adapter-related errors inherit from `AdapterError`, allowing the rest of
the platform to catch the base class when it does not need fine-grained
handling.
"""

from __future__ import annotations


class AdapterError(Exception):
    """Base class for all adapter-layer errors.

    Catch this to handle any error originating from the adapter subsystem.
    """

    def __init__(self, message: str = "", *, adapter_name: str = "") -> None:
        self.adapter_name = adapter_name
        super().__init__(message)


class InvocationError(AdapterError):
    """Raised when an adapter fails to execute an invoke or stream call.

    This covers framework-level failures (e.g., graph execution errors,
    tool call failures) as well as protocol-level mismatches.
    """


class TimeoutError(AdapterError):
    """Raised when an invocation exceeds its allotted timeout.

    Distinct from the built-in `TimeoutError` to avoid ambiguity in
    `except` clauses; this one always originates from the adapter layer.
    """


class FrameworkNotFoundError(AdapterError):
    """Raised when a requested framework adapter is not in the registry.

    Typically thrown during adapter lookup when the supplied name does not
    match any entry in `ADAPTER_REGISTRY`, or when the optional framework
    dependency is not installed.
    """
