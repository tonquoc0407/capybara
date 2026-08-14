import io
import json
import urllib.error
from unittest.mock import MagicMock, patch

import phoenix_export as pe


def _span(**over: object) -> dict:
    base = {
        "id": "global-1",
        "name": "chat",
        "context": {"trace_id": "t1", "span_id": "s1"},
        "span_kind": "LLM",
        "parent_id": None,
        "status_code": "OK",
        "attributes": {},
    }
    base.update(over)
    return base


def test_maps_known_span_kinds() -> None:
    for kind, want in [
        ("LLM", "llm"),
        ("EMBEDDING", "llm"),
        ("TOOL", "tool"),
        ("AGENT", "agent"),
        ("CHAIN", "agent"),
        ("RETRIEVER", "retrieval"),
        ("RERANKER", "retrieval"),
    ]:
        assert pe._span_line("t1", _span(span_kind=kind))["kind"] == want


def test_unmapped_span_kinds_fall_back_to_other() -> None:
    for kind in ("GUARDRAIL", "EVALUATOR", "UNKNOWN", ""):
        assert pe._span_line("t1", _span(span_kind=kind))["kind"] == "other"


def test_span_kind_lookup_is_case_insensitive() -> None:
    assert pe._span_line("t1", _span(span_kind="tool"))["kind"] == "tool"


def test_uses_context_span_id_not_the_global_id() -> None:
    line = pe._span_line("t1", _span())
    assert line["span"] == "s1"
    assert line["run"] == "t1"


def test_missing_context_falls_back_to_global_id() -> None:
    line = pe._span_line("t1", _span(context={}))
    assert line["span"] == "global-1"


def test_tool_kind_reads_tool_name_attribute() -> None:
    span = _span(span_kind="TOOL", attributes={"tool.name": "get_price"})
    assert pe._span_line("t1", span)["tool"] == "get_price"


def test_tool_kind_falls_back_to_span_name_without_tool_attribute() -> None:
    span = _span(span_kind="TOOL", name="get_price")
    assert pe._span_line("t1", span)["tool"] == "get_price"


def test_non_tool_kind_has_no_tool_field() -> None:
    assert "tool" not in pe._span_line("t1", _span(span_kind="LLM"))


def test_reads_model_provider_and_token_counts() -> None:
    span = _span(
        attributes={
            "llm.model_name": "gpt-4o",
            "llm.provider": "openai",
            "llm.token_count.prompt": 120,
            "llm.token_count.completion": 40,
        }
    )
    line = pe._span_line("t1", span)
    assert line["model"] == "gpt-4o"
    assert line["provider"] == "openai"
    assert line["tokens_in"] == 120
    assert line["tokens_out"] == 40


def test_missing_token_counts_default_to_zero() -> None:
    line = pe._span_line("t1", _span())
    assert line["tokens_in"] == 0
    assert line["tokens_out"] == 0


def test_non_numeric_token_count_defaults_to_zero() -> None:
    span = _span(attributes={"llm.token_count.prompt": "not-a-number"})
    assert pe._span_line("t1", span)["tokens_in"] == 0


def test_error_status_code_maps_to_error_others_stay_ok() -> None:
    assert pe._span_line("t1", _span(status_code="ERROR"))["status"] == "error"
    assert pe._span_line("t1", _span(status_code="OK"))["status"] == "ok"
    assert pe._span_line("t1", _span(status_code="UNSET"))["status"] == "ok"


def test_missing_timestamps_are_omitted() -> None:
    line = pe._span_line("t1", _span())
    assert "start" not in line
    assert "end" not in line


def test_present_timestamps_pass_through() -> None:
    span = _span(
        start_time="2026-08-13T10:00:00+00:00", end_time="2026-08-13T10:00:01+00:00"
    )
    line = pe._span_line("t1", span)
    assert line["start"] == "2026-08-13T10:00:00+00:00"
    assert line["end"] == "2026-08-13T10:00:01+00:00"


def test_llm_input_output_value_become_user_assistant_contents() -> None:
    span = _span(
        span_kind="LLM", attributes={"input.value": "hi", "output.value": {"a": 1}}
    )
    bodies = {c["role"]: c["body"] for c in pe._span_line("t1", span)["contents"]}
    assert bodies["user"] == "hi"
    assert json.loads(bodies["assistant"]) == {"a": 1}


def test_tool_input_output_value_become_input_output_contents() -> None:
    span = _span(
        span_kind="TOOL", attributes={"input.value": "hi", "output.value": "bye"}
    )
    bodies = {c["role"]: c["body"] for c in pe._span_line("t1", span)["contents"]}
    assert bodies["input"] == "hi"
    assert bodies["output"] == "bye"


def test_no_input_output_value_omits_contents_key() -> None:
    assert "contents" not in pe._span_line("t1", _span())


def _mock_response(body: dict) -> MagicMock:
    cm = MagicMock()
    cm.__enter__.return_value = io.BytesIO(json.dumps(body).encode())
    cm.__exit__.return_value = False
    return cm


def test_get_sends_bearer_auth_only_when_a_key_is_set() -> None:
    cfg = pe.Config(host="https://x.test", api_key="k")
    captured = {}

    def fake_urlopen(req):
        captured["auth"] = req.get_header("Authorization")
        return _mock_response({"data": []})

    with patch("urllib.request.urlopen", side_effect=fake_urlopen):
        pe._get(cfg, "/v1/projects/p/spans", {"trace_id": "t1"})
    assert captured["auth"] == "Bearer k"

    cfg_noauth = pe.Config(host="https://x.test", api_key=None)
    with patch("urllib.request.urlopen", side_effect=fake_urlopen):
        pe._get(cfg_noauth, "/v1/projects/p/spans", {"trace_id": "t1"})
    assert captured["auth"] is None


def test_get_wraps_http_error_as_system_exit() -> None:
    cfg = pe.Config(host="https://x.test", api_key=None)
    err = urllib.error.HTTPError("https://x.test", 401, "Unauthorized", None, None)
    with patch("urllib.request.urlopen", side_effect=err):
        try:
            pe._get(cfg, "/v1/projects/p/spans", {})
            raise AssertionError("expected SystemExit")
        except SystemExit:
            pass


def test_pagination_follows_next_cursor_until_absent() -> None:
    cfg = pe.Config(host="https://x.test", api_key=None)
    page1 = {"data": [{"id": "a"}], "next_cursor": "c2"}
    page2 = {"data": [{"id": "b"}], "next_cursor": None}
    with patch.object(pe, "_get", side_effect=[page1, page2]):
        spans = list(pe._spans(cfg, "proj", "t1"))
    assert [s["id"] for s in spans] == ["a", "b"]


def test_pagination_stops_on_empty_page() -> None:
    cfg = pe.Config(host="https://x.test", api_key=None)
    with patch.object(pe, "_get", return_value={"data": [], "next_cursor": "c2"}):
        assert list(pe._spans(cfg, "proj", "t1")) == []
