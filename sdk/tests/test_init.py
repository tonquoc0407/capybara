import json
import sys

import capybara
import capybara._otel as otel
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter


class FakeProxy:
    """Stands in for the default ProxyTracerProvider before any real provider."""


def test_init_creates_provider_when_absent(monkeypatch) -> None:
    created: list[TracerProvider] = []
    monkeypatch.setattr(otel.trace, "get_tracer_provider", lambda: FakeProxy())
    monkeypatch.setattr(otel.trace, "set_tracer_provider", lambda p: created.append(p))
    otel._configured = False

    provider = otel.init(endpoint="http://127.0.0.1:4318/v1/traces")
    assert isinstance(provider, TracerProvider)
    assert created == [provider]


def test_init_reuses_existing_provider(monkeypatch) -> None:
    existing = TracerProvider()
    monkeypatch.setattr(otel.trace, "get_tracer_provider", lambda: existing)
    monkeypatch.setattr(otel.trace, "set_tracer_provider", lambda p: pytest_fail())
    otel._configured = False

    provider = otel.init()
    assert provider is existing


def test_init_is_idempotent(monkeypatch) -> None:
    existing = TracerProvider()
    added: list[object] = []
    monkeypatch.setattr(otel.trace, "get_tracer_provider", lambda: existing)
    monkeypatch.setattr(existing, "add_span_processor", lambda p: added.append(p))
    otel._configured = False

    otel.init()
    otel.init()
    assert len(added) == 3  # entrypoint + schema processors and exporter, added once


# Time-travel re-executes the recorded process, so a run is only replayable if
# its root span says how it was launched.
def test_root_span_records_the_entrypoint(spans: InMemorySpanExporter) -> None:
    @capybara.trace(kind="agent", name="root")
    def root() -> int:
        return 1

    root()
    attributes = spans.get_finished_spans()[0].attributes
    assert json.loads(attributes[otel.ENTRYPOINT_ATTR])[0] == sys.executable
    assert attributes[otel.CWD_ATTR]


def test_child_spans_do_not_repeat_the_entrypoint(spans: InMemorySpanExporter) -> None:
    @capybara.trace(tool="inner")
    def inner() -> int:
        return 1

    @capybara.trace(kind="agent", name="outer")
    def outer() -> int:
        return inner()

    outer()
    by_name = {s.name: s for s in spans.get_finished_spans()}
    assert otel.ENTRYPOINT_ATTR not in by_name["execute_tool inner"].attributes


def pytest_fail() -> None:
    raise AssertionError(
        "set_tracer_provider must not be called when a provider exists"
    )
