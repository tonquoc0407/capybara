import io
import json
import urllib.error
from unittest.mock import MagicMock, patch

import langsmith_export as le


def _run(**over: object) -> dict:
    base = {
        "id": "run-1",
        "trace_id": "t1",
        "parent_run_id": None,
        "name": "chat",
        "run_type": "llm",
        "status": "success",
        "extra": {},
    }
    base.update(over)
    return base


def test_maps_known_run_types() -> None:
    for run_type, want in [
        ("llm", "llm"),
        ("embedding", "llm"),
        ("tool", "tool"),
        ("chain", "agent"),
        ("retriever", "retrieval"),
    ]:
        assert le._span_line(_run(run_type=run_type))["kind"] == want


def test_unmapped_run_types_fall_back_to_other() -> None:
    for run_type in ("prompt", "parser", "unknown", ""):
        assert le._span_line(_run(run_type=run_type))["kind"] == "other"


def test_run_type_lookup_is_case_insensitive() -> None:
    assert le._span_line(_run(run_type="TOOL"))["kind"] == "tool"


def test_ids_use_trace_id_and_run_id() -> None:
    line = le._span_line(_run())
    assert line["run"] == "t1"
    assert line["span"] == "run-1"


def test_missing_parent_becomes_empty_string() -> None:
    assert le._span_line(_run(parent_run_id=None))["parent"] == ""
    assert le._span_line(_run(parent_run_id="p1"))["parent"] == "p1"


def test_tool_kind_uses_run_name() -> None:
    assert le._span_line(_run(run_type="tool", name="get_price"))["tool"] == "get_price"


def test_non_tool_kind_has_no_tool_field() -> None:
    assert "tool" not in le._span_line(_run(run_type="llm"))


def test_reads_model_and_provider_from_extra_metadata() -> None:
    run = _run(extra={"metadata": {"ls_model_name": "gpt-4o", "ls_provider": "openai"}})
    line = le._span_line(run)
    assert line["model"] == "gpt-4o"
    assert line["provider"] == "openai"


def test_missing_extra_metadata_defaults_to_empty_strings() -> None:
    line = le._span_line(_run(extra=None))
    assert line["model"] == ""
    assert line["provider"] == ""


def test_reads_token_counts() -> None:
    line = le._span_line(_run(prompt_tokens=120, completion_tokens=40))
    assert line["tokens_in"] == 120
    assert line["tokens_out"] == 40


def test_missing_token_counts_default_to_zero() -> None:
    line = le._span_line(_run())
    assert line["tokens_in"] == 0
    assert line["tokens_out"] == 0


def test_non_numeric_token_count_defaults_to_zero() -> None:
    assert le._span_line(_run(prompt_tokens="not-a-number"))["tokens_in"] == 0


def test_error_status_maps_to_error_others_stay_ok() -> None:
    assert le._span_line(_run(status="error"))["status"] == "error"
    assert le._span_line(_run(status="success"))["status"] == "ok"
    assert le._span_line(_run(status="pending"))["status"] == "ok"


def test_missing_timestamps_are_omitted() -> None:
    line = le._span_line(_run())
    assert "start" not in line
    assert "end" not in line


def test_present_timestamps_pass_through() -> None:
    run = _run(start_time="2026-08-14T10:00:00Z", end_time="2026-08-14T10:00:01Z")
    line = le._span_line(run)
    assert line["start"] == "2026-08-14T10:00:00Z"
    assert line["end"] == "2026-08-14T10:00:01Z"


def test_llm_inputs_outputs_become_user_assistant_contents() -> None:
    run = _run(run_type="llm", inputs={"question": "hi"}, outputs={"answer": "hello"})
    bodies = {c["role"]: c["body"] for c in le._span_line(run)["contents"]}
    assert json.loads(bodies["user"]) == {"question": "hi"}
    assert json.loads(bodies["assistant"]) == {"answer": "hello"}


def test_tool_inputs_outputs_become_input_output_contents() -> None:
    run = _run(run_type="tool", inputs={"symbol": "NVDA"}, outputs={"price": 42})
    bodies = {c["role"]: c["body"] for c in le._span_line(run)["contents"]}
    assert json.loads(bodies["input"]) == {"symbol": "NVDA"}
    assert json.loads(bodies["output"]) == {"price": 42}


def test_no_inputs_outputs_omits_contents_key() -> None:
    assert "contents" not in le._span_line(_run())


def _mock_response(body: dict) -> MagicMock:
    cm = MagicMock()
    cm.__enter__.return_value = io.BytesIO(json.dumps(body).encode())
    cm.__exit__.return_value = False
    return cm


def test_post_sends_api_key_header_and_trace_filter() -> None:
    cfg = le.Config(host="https://x.test", api_key="k")
    captured = {}

    def fake_urlopen(req):
        captured["auth"] = req.get_header("X-api-key")
        captured["body"] = json.loads(req.data)
        return _mock_response({"runs": []})

    with patch("urllib.request.urlopen", side_effect=fake_urlopen):
        le._runs(cfg, "t1")
    assert captured["auth"] == "k"
    assert captured["body"] == {"trace": "t1"}


def test_post_wraps_http_error_as_system_exit() -> None:
    cfg = le.Config(host="https://x.test", api_key="k")
    err = urllib.error.HTTPError("https://x.test", 401, "Unauthorized", None, None)
    with patch("urllib.request.urlopen", side_effect=err):
        try:
            le._post(cfg, "/api/v1/runs/query", {})
            raise AssertionError("expected SystemExit")
        except SystemExit:
            pass


def test_runs_returns_query_response_runs() -> None:
    cfg = le.Config(host="https://x.test", api_key="k")
    with patch(
        "urllib.request.urlopen",
        return_value=_mock_response({"runs": [{"id": "a"}, {"id": "b"}]}),
    ):
        assert [r["id"] for r in le._runs(cfg, "t1")] == ["a", "b"]
