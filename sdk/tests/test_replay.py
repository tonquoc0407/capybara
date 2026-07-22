import json
from pathlib import Path

import pytest
from capybara._hash import hash_llm_request, hash_tool_call, message_text, value_text
from capybara.replay import MANIFEST_VERSION, ReplayError, Session, main

FIXTURE = Path(__file__).resolve().parents[2] / "internal/replay/testdata/hashes.json"


# The binary hashes recorded requests in Go; a mismatch here means no recorded
# run can be replayed.
@pytest.mark.skipif(not FIXTURE.exists(), reason="binary fixture not in this tree")
def test_hashes_match_the_go_fixture() -> None:
    fixture = json.loads(FIXTURE.read_text(encoding="utf8"))
    for case in fixture["llm"]:
        messages = [(m["role"], message_text(m["body"])) for m in case["messages"]]
        assert hash_llm_request(case["model"], messages) == case["hash"], case
    for case in fixture["tool"]:
        assert hash_tool_call(case["tool"], case["arguments"]) == case["hash"], case


def test_value_text_flattens_live_content_blocks() -> None:
    assert value_text("  hi  ") == "hi"
    assert value_text([{"type": "text", "text": "a"}, {"text": "b"}]) == "ab"
    assert value_text({"content": " nested "}) == "nested"


def test_unsupported_manifest_version_is_refused(tmp_path) -> None:
    path = tmp_path / "m.json"
    path.write_text(json.dumps({"version": MANIFEST_VERSION + 1}), encoding="utf8")
    assert main([str(path)]) == 1


def manifest(**over: object) -> dict:
    base = {
        "run_id": "0" * 32,
        "endpoint": "http://127.0.0.1:4318/v1/traces",
        "entrypoint": ["python", "agent.py"],
        "llm": [],
        "tools": [],
    }
    base.update(over)
    return base


# A run with nothing cached serializes as null from the binary, and must still
# report the determinism rule rather than crash.
def test_manifest_with_no_recording_reports_cleanly() -> None:
    session = Session(manifest(llm=None, tools=None))
    with pytest.raises(ReplayError, match="not in the recording"):
        session.serve_llm("claude-sonnet-5", [("user", "hi")])


def test_serves_recorded_tool_output() -> None:
    session = Session(
        manifest(
            tools=[
                {
                    "hash": hash_tool_call("lookup", '{"sku":"A"}'),
                    "span_id": "s1",
                    "tool": "lookup",
                    "output": '{"price": 42}',
                }
            ]
        )
    )
    assert session.serve_tool("lookup", '{"sku":"A"}') == {"price": 42}


def test_unrecorded_tool_call_stops_the_replay() -> None:
    session = Session(manifest())
    with pytest.raises(ReplayError, match="not in the recording"):
        session.serve_tool("lookup", '{"sku":"B"}')


def test_unrecorded_model_call_stops_before_the_edit() -> None:
    session = Session(manifest())
    with pytest.raises(ReplayError, match="no edit has been applied"):
        session.serve_llm("claude-sonnet-5", [("user", "hi")])


def test_model_call_goes_live_after_the_edit() -> None:
    edited = hash_tool_call("lookup", "{}")
    session = Session(
        manifest(
            tools=[
                {
                    "hash": edited,
                    "span_id": "s1",
                    "tool": "lookup",
                    "output": "{}",
                    "edited": True,
                }
            ]
        )
    )
    session.serve_tool("lookup", "{}")
    assert session.serve_llm("claude-sonnet-5", [("user", "hi")]) is None


def test_recorded_model_call_is_served_before_the_edit() -> None:
    entry = {
        "hash": hash_llm_request("m", [("user", "hi")]),
        "span_id": "s1",
        "model": "m",
        "response": '[{"type": "text", "content": "cached"}]',
    }
    session = Session(manifest(llm=[entry]))
    assert session.serve_llm("m", [("user", "hi")]) == entry
