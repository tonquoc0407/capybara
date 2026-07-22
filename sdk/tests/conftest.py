from collections.abc import Iterator

import capybara._otel as otel
import capybara._schema as schema_mod
import pytest
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

_exporter = InMemorySpanExporter()


@pytest.fixture(scope="session", autouse=True)
def _provider() -> Iterator[TracerProvider]:
    provider = TracerProvider()
    provider.add_span_processor(otel.EntrypointSpanProcessor())
    provider.add_span_processor(schema_mod.SchemaSpanProcessor())
    provider.add_span_processor(SimpleSpanProcessor(_exporter))
    trace.set_tracer_provider(provider)
    yield provider


@pytest.fixture(autouse=True)
def _reset() -> Iterator[None]:
    _exporter.clear()
    schema_mod._registry.clear()
    otel._configured = False
    yield
    _exporter.clear()
    schema_mod._registry.clear()


@pytest.fixture
def spans() -> InMemorySpanExporter:
    return _exporter
