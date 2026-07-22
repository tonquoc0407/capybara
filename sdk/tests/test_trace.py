import asyncio
import json

import capybara
import pytest
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.trace import StatusCode


def only_span(spans: InMemorySpanExporter):
    finished = spans.get_finished_spans()
    assert len(finished) == 1, finished
    return finished[0]


def test_sync_tool_records_io(spans: InMemorySpanExporter) -> None:
    @capybara.trace(tool="search_db")
    def search_db(query: str) -> dict:
        return {"price": 42, "results": [query]}

    assert search_db("gadget") == {"price": 42, "results": ["gadget"]}
    span = only_span(spans)
    assert span.name == "execute_tool search_db"
    attrs = span.attributes
    assert attrs["gen_ai.operation.name"] == "execute_tool"
    assert attrs["gen_ai.tool.name"] == "search_db"
    assert json.loads(attrs["gen_ai.tool.call.arguments"]) == {"query": "gadget"}
    assert json.loads(attrs["gen_ai.tool.call.result"]) == {
        "price": 42,
        "results": ["gadget"],
    }


def test_bare_decorator_uses_function_name(spans: InMemorySpanExporter) -> None:
    @capybara.trace
    def fetch_status(job: str) -> dict:
        return {"state": "done"}

    fetch_status("j-1")
    span = only_span(spans)
    assert span.name == "execute_tool fetch_status"
    assert span.attributes["gen_ai.tool.name"] == "fetch_status"


def test_agent_kind_has_no_tool_attrs(spans: InMemorySpanExporter) -> None:
    @capybara.trace(kind="agent", name="planner")
    def plan() -> str:
        return "planned"

    plan()
    span = only_span(spans)
    assert span.name == "planner"
    assert span.attributes["gen_ai.operation.name"] == "invoke_agent"
    assert "gen_ai.tool.name" not in span.attributes


def test_error_sets_error_status(spans: InMemorySpanExporter) -> None:
    @capybara.trace(tool="boom")
    def boom() -> None:
        raise ValueError("kaboom")

    with pytest.raises(ValueError, match="kaboom"):
        boom()
    span = only_span(spans)
    assert span.status.status_code == StatusCode.ERROR
    assert "gen_ai.tool.call.result" not in span.attributes


def test_async_tool_records_io(spans: InMemorySpanExporter) -> None:
    @capybara.trace(tool="afetch")
    async def afetch(url: str) -> dict:
        await asyncio.sleep(0)
        return {"ok": True}

    assert asyncio.run(afetch("http://x")) == {"ok": True}
    span = only_span(spans)
    assert span.attributes["gen_ai.tool.name"] == "afetch"
    assert json.loads(span.attributes["gen_ai.tool.call.result"]) == {"ok": True}


def test_nested_spans_nest(spans: InMemorySpanExporter) -> None:
    @capybara.trace(tool="inner")
    def inner() -> int:
        return 1

    @capybara.trace(kind="agent", name="outer")
    def outer() -> int:
        return inner()

    outer()
    finished = {s.name: s for s in spans.get_finished_spans()}
    inner_span, outer_span = finished["execute_tool inner"], finished["outer"]
    assert inner_span.parent is not None
    assert inner_span.parent.span_id == outer_span.context.span_id


def test_pydantic_result_serialized(spans: InMemorySpanExporter) -> None:
    from pydantic import BaseModel

    class Price(BaseModel):
        price: float

    @capybara.trace(tool="lookup")
    def lookup() -> Price:
        return Price(price=9.5)

    lookup()
    span = only_span(spans)
    assert json.loads(span.attributes["gen_ai.tool.call.result"]) == {"price": 9.5}
