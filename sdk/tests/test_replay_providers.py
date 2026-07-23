"""The provider patches, against the real clients they wrap."""

from __future__ import annotations

import json
from typing import Any

import pytest
from anthropic.resources.messages import AsyncMessages, Messages
from capybara._hash import hash_llm_request
from capybara.replay import ReplayError, Session, _patch_anthropic, _patch_openai
from openai.resources.chat.completions import AsyncCompletions, Completions

MESSAGES = [{"role": "user", "content": "what is the price?"}]
REPLY = json.dumps(
    [
        {"type": "text", "text": "Checking the catalogue."},
        {"type": "tool_use", "id": "toolu_1", "name": "fetch", "input": {"sku": "A"}},
    ]
)


def session_for(model: str) -> Session:
    return Session(
        {
            "llm": [
                {
                    "hash": hash_llm_request(model, [("user", "what is the price?")]),
                    "span_id": "llm1",
                    "model": model,
                    "response": REPLY,
                }
            ],
            "tools": [],
        }
    )


@pytest.fixture
def anthropic_patched(monkeypatch: pytest.MonkeyPatch) -> str:
    model = "claude-fable-5"
    monkeypatch.setattr(Messages, "create", Messages.create)
    monkeypatch.setattr(AsyncMessages, "create", AsyncMessages.create)
    monkeypatch.setattr(Messages, "stream", Messages.stream)
    monkeypatch.setattr(AsyncMessages, "stream", AsyncMessages.stream)
    _patch_anthropic(session_for(model))
    return model


@pytest.fixture
def openai_patched(monkeypatch: pytest.MonkeyPatch) -> str:
    model = "gpt-4o"
    monkeypatch.setattr(Completions, "create", Completions.create)
    monkeypatch.setattr(AsyncCompletions, "create", AsyncCompletions.create)
    _patch_openai(session_for(model))
    return model


def anthropic_events(model: str, **kwargs: Any) -> list[Any]:
    return list(Messages.create(object(), model=model, messages=MESSAGES, **kwargs))


def test_anthropic_create_serves_the_recording(anthropic_patched: str) -> None:
    msg = Messages.create(object(), model=anthropic_patched, messages=MESSAGES)
    assert [b.type for b in msg.content] == ["text", "tool_use"]
    assert msg.stop_reason == "tool_use"


def test_anthropic_stream_replays_the_event_sequence(anthropic_patched: str) -> None:
    events = anthropic_events(anthropic_patched, stream=True)
    assert [e.type for e in events] == [
        "message_start",
        "content_block_start",
        "content_block_delta",
        "content_block_stop",
        "content_block_start",
        "content_block_delta",
        "content_block_stop",
        "message_delta",
        "message_stop",
    ]


def test_anthropic_stream_deltas_reassemble(anthropic_patched: str) -> None:
    deltas = [
        e.delta
        for e in anthropic_events(anthropic_patched, stream=True)
        if e.type == "content_block_delta"
    ]
    text = "".join(d.text for d in deltas if hasattr(d, "text"))
    arguments = "".join(d.partial_json for d in deltas if hasattr(d, "partial_json"))
    assert text == "Checking the catalogue."
    assert json.loads(arguments) == {"sku": "A"}


def test_anthropic_async_paths(anthropic_patched: str) -> None:
    import asyncio

    msg = asyncio.run(
        AsyncMessages.create(object(), model=anthropic_patched, messages=MESSAGES)
    )
    assert msg.content[0].text == "Checking the catalogue."

    async def drain() -> list[str]:
        stream = await AsyncMessages.create(
            object(), model=anthropic_patched, messages=MESSAGES, stream=True
        )
        return [event.type async for event in stream]

    assert asyncio.run(drain())[0] == "message_start"


def test_anthropic_stream_helper_is_refused(anthropic_patched: str) -> None:
    with pytest.raises(ReplayError, match="cannot be replayed"):
        Messages.stream(object(), model=anthropic_patched, messages=MESSAGES)


def test_openai_create_serves_the_recording(openai_patched: str) -> None:
    completion = Completions.create(object(), model=openai_patched, messages=MESSAGES)
    choice = completion.choices[0]
    assert choice.message.content == "Checking the catalogue."
    assert choice.message.tool_calls[0].function.name == "fetch"
    assert choice.finish_reason == "tool_calls"


def test_openai_stream_replays_chunks(openai_patched: str) -> None:
    chunks = list(
        Completions.create(
            object(), model=openai_patched, messages=MESSAGES, stream=True
        )
    )
    content = "".join(c.choices[0].delta.content or "" for c in chunks)
    calls = [tc for c in chunks for tc in (c.choices[0].delta.tool_calls or ())]
    assert content == "Checking the catalogue."
    assert calls[0].function.name == "fetch"
    assert chunks[-1].choices[0].finish_reason == "tool_calls"


def test_openai_async_paths(openai_patched: str) -> None:
    import asyncio

    completion = asyncio.run(
        AsyncCompletions.create(object(), model=openai_patched, messages=MESSAGES)
    )
    assert completion.choices[0].message.content == "Checking the catalogue."

    async def drain() -> list[Any]:
        stream = await AsyncCompletions.create(
            object(), model=openai_patched, messages=MESSAGES, stream=True
        )
        return [chunk async for chunk in stream]

    assert len(asyncio.run(drain())) == 4
