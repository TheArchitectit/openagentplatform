"""
Adapter conformance test suite — runtime behavior.

Exercises invoke/stream/health/cancel/lifecycle against every registered
adapter. These tests require each adapter's SDK to be installed; tests for
unavailable SDKs are skipped at runtime.

Run: pytest tests/test_adapter_runtime.py -v
"""

from __future__ import annotations

import asyncio

from .conftest import (
    HealthStatus,
    InvokeResponse,
    StreamEvent,
    get_instance,
    make_request,
    skip_sdk,
)


# ---------------------------------------------------------------------------
# Conformance: Invoke (requires SDK — skipped if unavailable)
# ---------------------------------------------------------------------------


class TestInvokeConformance:
    def test_invoke_returns_response(self, adapter_name):
        instance = get_instance(adapter_name)
        req = make_request()
        try:
            resp = asyncio.run(instance.invoke(req))
        except Exception as e:
            skip_sdk(e, adapter_name)
        assert isinstance(resp, InvokeResponse)
        assert resp.task_id == req.task_id
        assert resp.status in ("completed", "failed", "input_required", "canceled")

    def test_invoke_has_messages(self, adapter_name):
        instance = get_instance(adapter_name)
        req = make_request()
        try:
            resp = asyncio.run(instance.invoke(req))
        except Exception as e:
            skip_sdk(e, adapter_name)
        if resp.status == "completed":
            assert len(resp.messages) > 0


# ---------------------------------------------------------------------------
# Conformance: Stream (requires SDK — skipped if unavailable)
# ---------------------------------------------------------------------------


class TestStreamConformance:
    def _collect_events(self, adapter_name: str) -> list[StreamEvent]:
        instance = get_instance(adapter_name)
        req = make_request()

        async def _run():
            events = []
            try:
                async for event in instance.stream(req):
                    events.append(event)
            except Exception as e:
                skip_sdk(e, adapter_name)
            return events

        return asyncio.run(_run())

    def test_stream_yields_events(self, adapter_name):
        events = self._collect_events(adapter_name)
        assert len(events) > 0

    def test_stream_ends_with_done(self, adapter_name):
        events = self._collect_events(adapter_name)
        assert events[-1].event_type == "done"

    def test_stream_events_are_stream_event(self, adapter_name):
        events = self._collect_events(adapter_name)
        for event in events:
            assert isinstance(event, StreamEvent)
            assert event.event_type in ("delta", "status", "error", "done")


# ---------------------------------------------------------------------------
# Conformance: Health
# ---------------------------------------------------------------------------


class TestHealthConformance:
    def test_health_returns_status(self, adapter_name):
        instance = get_instance(adapter_name)
        try:
            status = asyncio.run(instance.health())
        except Exception as e:
            skip_sdk(e, adapter_name)
        assert isinstance(status, HealthStatus)
        assert isinstance(status.healthy, bool)


# ---------------------------------------------------------------------------
# Conformance: Cancel
# ---------------------------------------------------------------------------


class TestCancelConformance:
    def test_cancel_returns_bool(self, adapter_name):
        instance = get_instance(adapter_name)
        try:
            result = asyncio.run(instance.cancel("nonexistent-task-id"))
        except Exception as e:
            skip_sdk(e, adapter_name)
        assert isinstance(result, bool)


# ---------------------------------------------------------------------------
# Conformance: Lifecycle
# ---------------------------------------------------------------------------


class TestLifecycleConformance:
    def test_start_stop_no_error(self, adapter_name):
        instance = get_instance(adapter_name)
        try:
            asyncio.run(instance.start())
            asyncio.run(instance.stop())
        except Exception as e:
            skip_sdk(e, adapter_name)
