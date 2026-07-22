import json

import capybara
from capybara._schema import SCHEMA_ATTR
from opentelemetry import trace
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from pydantic import BaseModel


class Price(BaseModel):
    price: float
    currency: str


def test_schema_registers_json_schema() -> None:
    capybara.schema("lookup_price", Price)
    from capybara._schema import schema_for

    stored = schema_for("lookup_price")
    assert stored is not None
    decoded = json.loads(stored)
    assert decoded["properties"]["price"]["type"] == "number"
    assert set(decoded["required"]) == {"price", "currency"}


def test_declared_schema_attached_via_trace(spans: InMemorySpanExporter) -> None:
    capybara.schema("lookup_price", Price)

    @capybara.trace(tool="lookup_price")
    def lookup_price(sku: str) -> dict:
        return {"price": 1.0, "currency": "USD"}

    lookup_price("x")
    span = spans.get_finished_spans()[0]
    assert json.loads(span.attributes[SCHEMA_ATTR])["required"]


def test_processor_attaches_to_preset_tool_name(spans: InMemorySpanExporter) -> None:
    capybara.schema("external_tool", Price)
    tracer = trace.get_tracer("test")
    # A third-party span that already carries the tool name at start.
    with tracer.start_as_current_span(
        "execute_tool external_tool",
        attributes={"gen_ai.tool.name": "external_tool"},
    ):
        pass
    span = spans.get_finished_spans()[0]
    assert SCHEMA_ATTR in span.attributes


def test_processor_ignores_unregistered_tool(spans: InMemorySpanExporter) -> None:
    tracer = trace.get_tracer("test")
    with tracer.start_as_current_span(
        "execute_tool other",
        attributes={"gen_ai.tool.name": "other"},
    ):
        pass
    span = spans.get_finished_spans()[0]
    assert SCHEMA_ATTR not in span.attributes
