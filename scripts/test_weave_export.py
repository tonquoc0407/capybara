import urllib.error
from unittest.mock import patch

import weave_export as we


def _call(**over: object) -> dict:
    base = {
        "id": "c1",
        "op_name": "openai.chat.completions.create",
        "parent_id": None,
        "inputs": {"messages": [{"role": "user", "content": "hello"}]},
        "output": "world",
        "summary": {
            "model": "gpt-4o",
            "usage": {"prompt_tokens": 10, "completion_tokens": 5},
        },
        "attributes": {},
    }
    base.update(over)
    return base


def test_maps_known_op_kinds() -> None:
    cases = [
        ("openai.chat.completions.create", "llm"),
        ("anthropic.messages.create", "llm"),
        ("search_tool", "tool"),
        ("vector_retrieve", "retrieval"),
        ("agent_loop", "agent"),
    ]
    for op, want in cases:
        call = _call(op_name=op, parent_id="root-1", inputs={})
        assert we._span_line("t1", call)["kind"] == want


def test_root_other_defaults_to_agent() -> None:
    call = _call(op_name="custom_func", parent_id=None, inputs={})
    assert we._span_line("t1", call)["kind"] == "agent"


def test_child_other_stays_other() -> None:
    call = _call(op_name="custom_func", parent_id="p1", inputs={})
    assert we._span_line("t1", call)["kind"] == "other"


def test_span_line_basic_fields() -> None:
    call = _call()
    line = we._span_line("t1", call)
    assert line["run"] == "t1"
    assert line["span"] == "c1"
    assert line["model"] == "gpt-4o"
    assert line["provider"] == "openai"
    assert line["tokens_in"] == 10
    assert line["tokens_out"] == 5
    assert line["status"] == "ok"


def test_exception_maps_to_error() -> None:
    call = _call(exception="RateLimitError")
    assert we._span_line("t1", call)["status"] == "error"


def test_tool_kind_sets_tool_field() -> None:
    call = _call(
        op_name="weather_tool",
        parent_id="p1",
        attributes={"tool.name": "get_weather"},
    )
    line = we._span_line("t1", call)
    assert line["kind"] == "tool"
    assert line["tool"] == "get_weather"


def test_nested_usage_extraction() -> None:
    call = _call(
        summary={"usage": {"openai": {"prompt_tokens": 42, "completion_tokens": 17}}}
    )
    line = we._span_line("t1", call)
    assert line["tokens_in"] == 42
    assert line["tokens_out"] == 17


def test_auth_header_basic_and_bearer() -> None:
    assert we._auth_header("Bearer token123") == "Bearer token123"
    header = we._auth_header("key123")
    assert header.startswith("Basic ")


def test_post_wraps_http_error_as_system_exit() -> None:
    cfg = we.Config(host="https://weave.test", api_key="k")
    err = urllib.error.HTTPError("https://weave.test", 401, "Unauthorized", None, None)
    with patch("urllib.request.urlopen", side_effect=err):
        try:
            we._post(cfg, "/call/query", {})
            raise AssertionError("expected SystemExit")
        except SystemExit:
            pass


def test_calls_yields_list_and_dict() -> None:
    cfg = we.Config(host="https://weave.test", api_key="k")
    with patch.object(we, "_post", return_value=[{"id": "c1"}, {"id": "c2"}]):
        res = list(we._calls(cfg, "proj", "t1"))
        assert len(res) == 2

    with patch.object(we, "_post", return_value={"calls": [{"id": "c3"}]}):
        res = list(we._calls(cfg, "proj", "t1"))
        assert len(res) == 1
