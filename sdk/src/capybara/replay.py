"""Replay runner: re-executes a recorded run against its own recording."""

from __future__ import annotations

import json
import runpy
import sys
from collections.abc import AsyncIterator, Iterator
from typing import Any

from opentelemetry import trace
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.trace.id_generator import RandomIdGenerator

from . import _otel, _trace
from ._hash import hash_llm_request, hash_tool_call, value_text
from ._schema import SchemaSpanProcessor

# The binary and this package are released separately, so a manifest it cannot
# read must say so rather than replay half of it.
MANIFEST_VERSION = 1


class ReplayError(RuntimeError):
    """A call the recording cannot serve; the replay stops rather than guess."""


class Session:
    """Serves a manifest's recording to the process being replayed."""

    def __init__(self, manifest: dict[str, Any]) -> None:
        self.manifest = manifest
        # A recording with nothing to serve arrives as null, not an empty list.
        llm = manifest.get("llm") or ()
        tools = manifest.get("tools") or ()
        self.llm: dict[str, dict[str, Any]] = {e["hash"]: e for e in llm}
        self.tools: dict[str, dict[str, Any]] = {e["hash"]: e for e in tools}
        self.edited = next((e["hash"] for e in tools if e.get("edited")), None)
        self.diverged = False

    def serve_tool(self, tool: str, arguments: str) -> Any:
        entry = self.tools.get(hash_tool_call(tool, arguments))
        if entry is None:
            raise ReplayError(
                f"tool {tool} was called with arguments that are not in the recording"
            )
        if entry["hash"] == self.edited:
            self.diverged = True
        return _decode(entry["output"])

    # A model call missing from the recording is only legitimate after the
    # edited value has been served; before that the replay is not deterministic.
    def serve_llm(
        self, model: str, messages: list[tuple[str, Any]]
    ) -> dict[str, Any] | None:
        pairs = [(role, value_text(content)) for role, content in messages]
        entry = self.llm.get(hash_llm_request(model, pairs))
        if entry is not None:
            return entry
        if not self.diverged:
            raise ReplayError(
                f"model call to {model} is not in the recording and no edit has "
                "been applied yet"
            )
        return None

    def install(self) -> None:
        _trace.tool_server = self.serve_tool
        _patch_anthropic(self)
        _patch_openai(self)

    def execute(self) -> None:
        entrypoint = self.manifest["entrypoint"]
        script = entrypoint[1] if len(entrypoint) > 1 else None
        if script is None:
            raise ReplayError("recorded entrypoint has no script to run")
        sys.argv = list(entrypoint[1:])
        runpy.run_path(script, run_name="__main__")


def provider_for(manifest: dict[str, Any]) -> TracerProvider:
    """Install a provider whose trace id is the run id capybara is waiting on."""
    provider = TracerProvider(
        resource=Resource.create({"service.name": "capybara-replay"}),
        id_generator=_FixedTrace(int(manifest["run_id"], 16)),
    )
    provider.add_span_processor(_otel.EntrypointSpanProcessor())
    provider.add_span_processor(SchemaSpanProcessor())
    provider.add_span_processor(BatchSpanProcessor(_exporter(manifest["endpoint"])))
    trace.set_tracer_provider(provider)
    # The replayed script calls capybara.init(); leave it this provider.
    _otel._configured = True
    return provider


class _FixedTrace(RandomIdGenerator):
    def __init__(self, trace_id: int) -> None:
        self._trace_id = trace_id

    def generate_trace_id(self) -> int:
        return self._trace_id


def _exporter(endpoint: str) -> Any:
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter

    return OTLPSpanExporter(endpoint=endpoint)


def _decode(body: str) -> Any:
    try:
        return json.loads(body)
    except ValueError:
        return body


def main(argv: list[str] | None = None) -> int:
    """Run the manifest named on the command line; used as python -m capybara.replay."""
    args = sys.argv[1:] if argv is None else argv
    if len(args) != 1:
        print("usage: python -m capybara.replay <manifest.json>", file=sys.stderr)
        return 2
    with open(args[0], encoding="utf8") as f:
        manifest = json.load(f)
    version = manifest.get("version")
    if version != MANIFEST_VERSION:
        print(
            f"replay stopped: manifest version {version} needs a newer capybara-sdk",
            file=sys.stderr,
        )
        return 1
    session = Session(manifest)
    provider = provider_for(manifest)
    session.install()
    try:
        session.execute()
    except ReplayError as err:
        print(f"replay stopped: {err}", file=sys.stderr)
        return 1
    finally:
        provider.shutdown()
    return 0


def _patch_anthropic(session: Session) -> None:
    try:
        from anthropic.resources.messages import AsyncMessages, Messages
    except ImportError:
        return
    original, async_original = Messages.create, AsyncMessages.create

    def create(self: Any, **kwargs: Any) -> Any:
        model = kwargs.get("model", "")
        entry = session.serve_llm(model, _anthropic_messages(kwargs))
        if entry is None:
            return original(self, **kwargs)
        if kwargs.get("stream"):
            return _anthropic_events(entry, model)
        return _anthropic_response(entry, model)

    async def acreate(self: Any, **kwargs: Any) -> Any:
        model = kwargs.get("model", "")
        entry = session.serve_llm(model, _anthropic_messages(kwargs))
        if entry is None:
            return await async_original(self, **kwargs)
        if kwargs.get("stream"):
            return _aiter(_anthropic_events(entry, model))
        return _anthropic_response(entry, model)

    # The messages.stream() helper builds its reader straight from the
    # transport, so a patch here would never see it. Refusing beats going
    # live behind the determinism rule.
    def stream(self: Any, **kwargs: Any) -> Any:
        raise ReplayError(
            "messages.stream() cannot be replayed; use create(stream=True)"
        )

    Messages.create = create  # type: ignore[method-assign]
    AsyncMessages.create = acreate  # type: ignore[method-assign]
    Messages.stream = stream  # type: ignore[method-assign]
    AsyncMessages.stream = stream  # type: ignore[method-assign]


def _anthropic_events(entry: dict[str, Any], model: str) -> Iterator[Any]:
    from anthropic import types

    message = _anthropic_response(entry, model)
    empty = message.model_copy(update={"content": [], "stop_reason": None})
    yield types.RawMessageStartEvent(type="message_start", message=empty)
    for index, block in enumerate(message.content):
        yield types.RawContentBlockStartEvent(
            type="content_block_start", index=index, content_block=block
        )
        yield types.RawContentBlockDeltaEvent(
            type="content_block_delta", index=index, delta=_anthropic_delta(block)
        )
        yield types.RawContentBlockStopEvent(type="content_block_stop", index=index)
    yield types.RawMessageDeltaEvent.model_validate(
        {
            "type": "message_delta",
            "delta": {"stop_reason": message.stop_reason, "stop_sequence": None},
            "usage": {"output_tokens": 0},
        }
    )
    yield types.RawMessageStopEvent(type="message_stop")


def _anthropic_delta(block: Any) -> Any:
    from anthropic import types

    if block.type == "tool_use":
        return types.InputJSONDelta(
            type="input_json_delta", partial_json=json.dumps(block.input)
        )
    return types.TextDelta(type="text_delta", text=block.text)


def _anthropic_messages(kwargs: dict[str, Any]) -> list[tuple[str, Any]]:
    messages: list[tuple[str, Any]] = []
    system = kwargs.get("system")
    if system:
        messages.append(("system", system))
    for message in kwargs.get("messages", ()):
        messages.append((message.get("role", "user"), message.get("content")))
    return messages


def _anthropic_response(entry: dict[str, Any], model: str) -> Any:
    from anthropic.types import Message

    blocks = []
    for part in _parts(entry["response"]):
        if part["type"] == "tool_use":
            blocks.append(part)
        else:
            blocks.append({"type": "text", "text": part["text"]})
    stop = "tool_use" if any(b["type"] == "tool_use" for b in blocks) else "end_turn"
    return Message.model_validate(
        {
            "id": f"msg_replay_{entry['span_id']}",
            "type": "message",
            "role": "assistant",
            "model": entry.get("model") or model,
            "content": blocks,
            "stop_reason": stop,
            "stop_sequence": None,
            "usage": {"input_tokens": 0, "output_tokens": 0},
        }
    )


def _patch_openai(session: Session) -> None:
    try:
        from openai.resources.chat.completions import AsyncCompletions, Completions
    except ImportError:
        return
    original, async_original = Completions.create, AsyncCompletions.create

    def create(self: Any, **kwargs: Any) -> Any:
        model = kwargs.get("model", "")
        entry = session.serve_llm(model, _openai_messages(kwargs))
        if entry is None:
            return original(self, **kwargs)
        if kwargs.get("stream"):
            return _openai_chunks(entry, model)
        return _openai_response(entry, model)

    async def acreate(self: Any, **kwargs: Any) -> Any:
        model = kwargs.get("model", "")
        entry = session.serve_llm(model, _openai_messages(kwargs))
        if entry is None:
            return await async_original(self, **kwargs)
        if kwargs.get("stream"):
            return _aiter(_openai_chunks(entry, model))
        return _openai_response(entry, model)

    Completions.create = create  # type: ignore[method-assign]
    AsyncCompletions.create = acreate  # type: ignore[method-assign]


def _openai_messages(kwargs: dict[str, Any]) -> list[tuple[str, Any]]:
    return [
        (m.get("role", "user"), m.get("content")) for m in kwargs.get("messages", ())
    ]


def _openai_chunks(entry: dict[str, Any], model: str) -> Iterator[Any]:
    from openai.types.chat import ChatCompletionChunk

    completion = _openai_response(entry, model)
    message = completion.choices[0].message
    base = {
        "id": completion.id,
        "object": "chat.completion.chunk",
        "created": 0,
        "model": completion.model,
    }

    def chunk(delta: dict[str, Any], finish: str | None = None) -> Any:
        return ChatCompletionChunk.model_validate(
            {**base, "choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}
        )

    yield chunk({"role": "assistant"})
    if message.content:
        yield chunk({"content": message.content})
    for index, call in enumerate(message.tool_calls or ()):
        yield chunk(
            {
                "tool_calls": [
                    {
                        "index": index,
                        "id": call.id,
                        "type": "function",
                        "function": {
                            "name": call.function.name,
                            "arguments": call.function.arguments,
                        },
                    }
                ]
            }
        )
    yield chunk({}, completion.choices[0].finish_reason)


def _openai_response(entry: dict[str, Any], model: str) -> Any:
    from openai.types.chat import ChatCompletion

    text, tool_calls = "", []
    for part in _parts(entry["response"]):
        if part["type"] == "tool_use":
            tool_calls.append(
                {
                    "id": part["id"],
                    "type": "function",
                    "function": {
                        "name": part["name"],
                        "arguments": json.dumps(part["input"]),
                    },
                }
            )
        else:
            text += part["text"]
    message: dict[str, Any] = {"role": "assistant", "content": text or None}
    if tool_calls:
        message["tool_calls"] = tool_calls
    return ChatCompletion.model_validate(
        {
            "id": f"chatcmpl_replay_{entry['span_id']}",
            "object": "chat.completion",
            "created": 0,
            "model": entry.get("model") or model,
            "choices": [
                {
                    "index": 0,
                    "message": message,
                    "finish_reason": "tool_calls" if tool_calls else "stop",
                }
            ],
        }
    )


async def _aiter(events: Iterator[Any]) -> AsyncIterator[Any]:
    for event in events:
        yield event


# Recorded replies are the instrumentor's part list; normalize the two shapes
# capybara can serve back to a provider client.
def _parts(response: str) -> list[dict[str, Any]]:
    value = _decode(response)
    if isinstance(value, str):
        return [{"type": "text", "text": value}]
    if isinstance(value, dict):
        value = [value]
    parts: list[dict[str, Any]] = []
    for element in value:
        if not isinstance(element, dict):
            parts.append({"type": "text", "text": str(element)})
            continue
        kind = element.get("type")
        if kind in ("tool_call", "tool_use"):
            arguments = element.get("input", element.get("arguments", {}))
            if isinstance(arguments, str):
                arguments = _decode(arguments)
            parts.append(
                {
                    "type": "tool_use",
                    "id": element.get("id", "toolu_replay"),
                    "name": element.get("name", ""),
                    "input": arguments,
                }
            )
        else:
            parts.append({"type": "text", "text": value_text(element)})
    return parts


if __name__ == "__main__":
    raise SystemExit(main())
