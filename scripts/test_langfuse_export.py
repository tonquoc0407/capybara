import io
import json
import urllib.error
from unittest.mock import MagicMock, patch

import langfuse_export as le


def test_maps_known_types_to_capybara_kinds() -> None:
    for lf_type, want in [
        ("AGENT", "agent"),
        ("TOOL", "tool"),
        ("RETRIEVER", "retrieval"),
        ("GENERATION", "llm"),
        ("EMBEDDING", "llm"),
    ]:
        assert le._span_line("run", {"id": "s", "type": lf_type})["kind"] == want


def test_unmapped_types_fall_back_to_other() -> None:
    for lf_type in ("SPAN", "EVENT", "CHAIN", "EVALUATOR", "GUARDRAIL", ""):
        assert le._span_line("run", {"id": "s", "type": lf_type})["kind"] == "other"


def test_type_lookup_is_case_insensitive() -> None:
    assert le._span_line("run", {"id": "s", "type": "tool"})["kind"] == "tool"


def test_name_falls_back_to_lowercased_type_when_missing() -> None:
    line = le._span_line("run", {"id": "s", "type": "GENERATION"})
    assert line["name"] == "generation"


def test_tool_kind_carries_its_own_name_as_tool_field() -> None:
    line = le._span_line("run", {"id": "s", "type": "TOOL", "name": "get_price"})
    assert line["tool"] == "get_price"


def test_non_tool_kind_has_no_tool_field() -> None:
    line = le._span_line("run", {"id": "s", "type": "GENERATION", "name": "chat"})
    assert "tool" not in line


def test_missing_usage_defaults_tokens_to_zero() -> None:
    line = le._span_line("run", {"id": "s", "type": "GENERATION"})
    assert line["tokens_in"] == 0
    assert line["tokens_out"] == 0


def test_error_level_maps_to_error_status_others_stay_ok() -> None:
    assert le._span_line("run", {"id": "s", "level": "ERROR"})["status"] == "error"
    assert le._span_line("run", {"id": "s", "level": "DEFAULT"})["status"] == "ok"
    assert le._span_line("run", {"id": "s"})["status"] == "ok"


def test_missing_timestamps_are_omitted_not_sent_blank() -> None:
    line = le._span_line("run", {"id": "s"})
    assert "start" not in line
    assert "end" not in line


def test_object_input_output_serialize_to_json_text() -> None:
    line = le._span_line("run", {"id": "s", "input": {"sku": "AAPL"}, "output": [1, 2]})
    bodies = {c["role"]: c["body"] for c in line["contents"]}
    assert json.loads(bodies["input"]) == {"sku": "AAPL"}
    assert json.loads(bodies["output"]) == [1, 2]


def test_string_input_output_pass_through_unquoted() -> None:
    line = le._span_line("run", {"id": "s", "input": "hi", "output": "there"})
    bodies = {c["role"]: c["body"] for c in line["contents"]}
    assert bodies == {"input": "hi", "output": "there"}


def test_no_input_output_omits_contents_key() -> None:
    assert "contents" not in le._span_line("run", {"id": "s"})


def test_attrs_drop_empty_values() -> None:
    line = le._span_line("run", {"id": "s", "environment": "prod", "version": ""})
    assert line["attrs"] == {"environment": "prod"}


def _mock_response(body: dict) -> MagicMock:
    cm = MagicMock()
    cm.__enter__.return_value = io.BytesIO(json.dumps(body).encode())
    cm.__exit__.return_value = False
    return cm


def test_get_sends_basic_auth_and_query_params() -> None:
    cfg = le.Config(host="https://x.test", public_key="pk", secret_key="sk")
    captured = {}

    def fake_urlopen(req):
        captured["url"] = req.full_url
        captured["auth"] = req.get_header("Authorization")
        return _mock_response({"data": []})

    with patch("urllib.request.urlopen", side_effect=fake_urlopen):
        le._get(cfg, "/api/public/observations", {"traceId": "t1", "page": 1})

    assert "traceId=t1" in captured["url"]
    assert captured["auth"].startswith("Basic ")


def test_get_wraps_http_error_as_system_exit() -> None:
    cfg = le.Config(host="https://x.test", public_key="pk", secret_key="sk")
    err = urllib.error.HTTPError("https://x.test", 401, "Unauthorized", None, None)
    with patch("urllib.request.urlopen", side_effect=err):
        try:
            le._get(cfg, "/api/public/observations", {})
            raise AssertionError("expected SystemExit")
        except SystemExit:
            pass


def test_pagination_stops_at_a_short_page() -> None:
    cfg = le.Config(host="https://x.test", public_key="pk", secret_key="sk")
    full_page = {"data": [{"id": f"o{i}"} for i in range(le._PAGE_LIMIT)]}
    last_page = {"data": [{"id": "last"}]}
    with patch.object(le, "_get", side_effect=[full_page, last_page]):
        obs = list(le._observations(cfg, "t1"))
    assert [o["id"] for o in obs] == [f"o{i}" for i in range(le._PAGE_LIMIT)] + ["last"]


def test_pagination_stops_on_empty_page() -> None:
    cfg = le.Config(host="https://x.test", public_key="pk", secret_key="sk")
    with patch.object(le, "_get", return_value={"data": []}):
        assert list(le._observations(cfg, "t1")) == []


def test_require_env_raises_clearly_when_missing(monkeypatch) -> None:
    monkeypatch.delenv("DOES_NOT_EXIST_XYZ", raising=False)
    try:
        le._require_env("DOES_NOT_EXIST_XYZ")
        raise AssertionError("expected SystemExit")
    except SystemExit:
        pass
