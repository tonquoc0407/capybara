"""Reproduce openai-agents-python issue #4070 and export its traces for capybara.

Run with openai-agents-python v0.19.1 checked out locally. The agent uses the
upstream repository's FakeModel and test helpers, so it makes no API calls.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
from pathlib import Path
from typing import Any


def import_upstream(repo: Path) -> tuple[Any, ...]:
    sys.path.insert(0, str(repo.resolve()))
    from agents import Agent, Runner
    from agents.tracing.provider import DefaultTraceProvider
    from agents.tracing.setup import set_trace_provider
    from tests.fake_model import FakeModel
    from tests.test_responses import get_function_tool, get_function_tool_call, get_text_message
    from tests.testing_processor import SPAN_PROCESSOR_TESTING

    provider = DefaultTraceProvider()
    provider.set_processors([SPAN_PROCESSOR_TESTING])
    set_trace_provider(provider)
    return (
        Agent,
        Runner,
        FakeModel,
        get_function_tool,
        get_function_tool_call,
        get_text_message,
        SPAN_PROCESSOR_TESTING,
    )


async def capture(mode: str, upstream: tuple[Any, ...]) -> list[dict[str, Any]]:
    Agent, Runner, FakeModel, get_tool, get_call, get_message, processor = upstream
    processor.clear()

    model = FakeModel(tracing_enabled=True)
    if mode == "tool_then_error":
        model.add_multiple_turn_outputs(
            [[get_message("Checking the quote"), get_call("lookup_quote", "{}")], RuntimeError("boom")]
        )
        agent = Agent(name="quote_agent", model=model, tools=[get_tool("lookup_quote", "42 USD")])
    else:
        model.set_next_output(RuntimeError("boom"))
        agent = Agent(name="quote_agent", model=model)
    try:
        if mode == "streamed":
            result = Runner.run_streamed(agent, input="Find the quote")
            async for _ in result.stream_events():
                pass
        else:
            await Runner.run(agent, input="Find the quote")
    except RuntimeError as exc:
        if str(exc) != "boom":
            raise
    else:
        raise AssertionError(f"{mode} agent did not raise the injected error")

    exported = [span.export() for span in processor.get_ordered_spans()]
    spans = [span for span in exported if span is not None]
    agent_errors = [
        span.get("error") for span in spans if span.get("span_data", {}).get("type") == "agent"
    ]
    assert len(agent_errors) == 1
    assert (agent_errors[0] is not None) == (mode == "streamed"), agent_errors
    print(f"{mode}: {len(spans)} spans; agent error={agent_errors[0] is not None}")

    lines = []
    for span in spans:
        data = span.get("span_data") or {}
        span_type = data.get("type", "other")
        kind = {"agent": "agent", "generation": "llm", "function": "tool"}.get(
            span_type, "other"
        )
        line = {
            "run": span["trace_id"],
            "span": span["id"],
            "parent": span.get("parent_id") or "",
            "kind": kind,
            "name": data.get("name") or span_type,
            "status": "error" if span.get("error") else "ok",
            "attrs": {"source": "openai-agents-python-v0.19.1", "sdk_error": span.get("error")},
        }
        if kind == "tool":
            line["tool"] = data.get("name") or ""
            line["contents"] = [
                {"role": role, "body": str(data[key])}
                for role, key in (("input", "input"), ("output", "output"))
                if data.get(key) is not None
            ]
        for key, source in (("start", "started_at"), ("end", "ended_at")):
            if span.get(source):
                line[key] = span[source]
        lines.append(line)
    return lines


async def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("agents_repo", type=Path, help="checkout of openai-agents-python v0.19.1")
    parser.add_argument("output", type=Path, help="capybara span-per-line JSONL output")
    args = parser.parse_args()
    upstream = import_upstream(args.agents_repo)
    lines = (
        await capture("regular", upstream)
        + await capture("streamed", upstream)
        + await capture("tool_then_error", upstream)
    )
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(
        "".join(json.dumps(line, ensure_ascii=False) + "\n" for line in lines),
        encoding="utf-8",
    )
    print(f"wrote {len(lines)} spans to {args.output}")


if __name__ == "__main__":
    # asyncio.run() stalls during loop shutdown after this version's streamed failure.
    loop = asyncio.new_event_loop()
    try:
        asyncio.set_event_loop(loop)
        loop.run_until_complete(main())
    finally:
        loop.close()
        asyncio.set_event_loop(None)
