#!/usr/bin/env python3
"""Fetch an Arize Phoenix trace and emit capybara's import jsonl."""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any
from urllib.parse import urlencode

_PAGE_LIMIT = 1000

# Phoenix labels its own spans with the OpenInference attributes capybara's
# otlp receiver already reads (internal/ingest/otlp/semconv.go); reusing those
# exact keys keeps this mapping in step with the one the binary trusts.
_KIND_BY_SPAN_KIND = {
    "LLM": "llm",
    "EMBEDDING": "llm",
    "TOOL": "tool",
    "AGENT": "agent",
    "CHAIN": "agent",
    "RETRIEVER": "retrieval",
    "RERANKER": "retrieval",
}


@dataclass
class Config:
    host: str
    api_key: str | None


def main() -> None:
    args = _parse_args()
    cfg = Config(host=args.host.rstrip("/"), api_key=args.api_key)
    out = sys.stdout if args.out == "-" else open(args.out, "w", encoding="utf8")
    try:
        for trace_id in args.trace_id:
            n = 0
            for span in _spans(cfg, args.project, trace_id):
                out.write(
                    json.dumps(_span_line(trace_id, span), ensure_ascii=False) + "\n"
                )
                n += 1
            if n == 0:
                print(f"warning: no spans for trace {trace_id}", file=sys.stderr)
    finally:
        if out is not sys.stdout:
            out.close()


def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("project", help="Phoenix project identifier")
    p.add_argument("trace_id", nargs="+", help="trace id(s) to export")
    p.add_argument(
        "--host", default=os.environ.get("PHOENIX_HOST", "http://localhost:6006")
    )
    p.add_argument("--api-key", default=os.environ.get("PHOENIX_API_KEY"))
    p.add_argument("--out", default="-", help="output path, or - for stdout")
    return p.parse_args()


def _spans(cfg: Config, project: str, trace_id: str):
    cursor = None
    while True:
        params: dict[str, Any] = {"trace_id": trace_id, "limit": _PAGE_LIMIT}
        if cursor:
            params["cursor"] = cursor
        body = _get(cfg, f"/v1/projects/{project}/spans", params)
        items = body.get("data", [])
        yield from items
        cursor = body.get("next_cursor")
        if not cursor or not items:
            return


def _get(cfg: Config, path: str, params: dict[str, Any]) -> dict[str, Any]:
    url = f"{cfg.host}{path}?{urlencode(params, doseq=True)}"
    req = urllib.request.Request(url)
    if cfg.api_key:
        req.add_header("Authorization", f"Bearer {cfg.api_key}")
    try:
        with urllib.request.urlopen(req) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        raise SystemExit(f"phoenix {path}: {e.code} {e.reason}") from e


def _span_line(trace_id: str, span: dict[str, Any]) -> dict[str, Any]:
    attrs = span.get("attributes") or {}
    context = span.get("context") or {}
    kind = _KIND_BY_SPAN_KIND.get(str(span.get("span_kind", "")).upper(), "other")
    name = span.get("name") or ""
    line: dict[str, Any] = {
        "run": trace_id,
        "span": context.get("span_id") or span.get("id", ""),
        "parent": span.get("parent_id") or "",
        "kind": kind,
        "name": name,
        "tokens_in": _int(attrs.get("llm.token_count.prompt")),
        "tokens_out": _int(attrs.get("llm.token_count.completion")),
        "status": "error" if span.get("status_code") == "ERROR" else "ok",
        "model": attrs.get("llm.model_name") or "",
        "provider": attrs.get("llm.provider") or "",
        "attrs": {},
    }
    if kind == "tool":
        line["tool"] = attrs.get("tool.name") or name
    if span.get("start_time"):
        line["start"] = span["start_time"]
    if span.get("end_time"):
        line["end"] = span["end_time"]
    contents = _contents(attrs)
    if contents:
        line["contents"] = contents
    return line


def _int(value: Any) -> int:
    try:
        return int(value)
    except (TypeError, ValueError):
        return 0


def _contents(attrs: dict[str, Any]) -> list[dict[str, str]]:
    contents = []
    for role, key in (("input", "input.value"), ("output", "output.value")):
        body = attrs.get(key)
        if body in (None, ""):
            continue
        text = body if isinstance(body, str) else json.dumps(body, ensure_ascii=False)
        contents.append({"role": role, "body": text})
    return contents


if __name__ == "__main__":
    main()
