#!/usr/bin/env python3
"""Fetch a LangSmith trace and emit capybara's import jsonl."""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any

_KIND_BY_RUN_TYPE = {
    "LLM": "llm",
    "EMBEDDING": "llm",
    "TOOL": "tool",
    "CHAIN": "agent",
    "RETRIEVER": "retrieval",
}


@dataclass
class Config:
    host: str
    api_key: str


def main() -> None:
    args = _parse_args()
    cfg = Config(host=args.host.rstrip("/"), api_key=args.api_key)
    out = sys.stdout if args.out == "-" else open(args.out, "w", encoding="utf8")
    try:
        for trace_id in args.trace_id:
            n = 0
            for run in _runs(cfg, trace_id):
                out.write(json.dumps(_span_line(run), ensure_ascii=False) + "\n")
                n += 1
            if n == 0:
                print(f"warning: no runs for trace {trace_id}", file=sys.stderr)
    finally:
        if out is not sys.stdout:
            out.close()


def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("trace_id", nargs="+", help="trace id(s) to export")
    p.add_argument(
        "--host",
        default=os.environ.get("LANGSMITH_ENDPOINT", "https://api.smith.langchain.com"),
    )
    p.add_argument("--api-key", default=os.environ.get("LANGSMITH_API_KEY"))
    p.add_argument("--out", default="-", help="output path, or - for stdout")
    args = p.parse_args()
    if not args.api_key:
        p.error("no api key: pass --api-key or set LANGSMITH_API_KEY")
    return args


def _runs(cfg: Config, trace_id: str) -> list[dict[str, Any]]:
    # Filtering by trace disables the query's cursor pagination entirely
    # (LangSmith's own /runs/query docs), so one call returns the whole trace.
    body = _post(cfg, "/api/v1/runs/query", {"trace": trace_id})
    return body.get("runs", [])


def _post(cfg: Config, path: str, payload: dict[str, Any]) -> dict[str, Any]:
    req = urllib.request.Request(
        f"{cfg.host}{path}",
        data=json.dumps(payload).encode(),
        method="POST",
    )
    req.add_header("Content-Type", "application/json")
    req.add_header("X-Api-Key", cfg.api_key)
    try:
        with urllib.request.urlopen(req) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        raise SystemExit(f"langsmith {path}: {e.code} {e.reason}") from e


def _span_line(run: dict[str, Any]) -> dict[str, Any]:
    metadata = ((run.get("extra") or {}).get("metadata")) or {}
    kind = _KIND_BY_RUN_TYPE.get(str(run.get("run_type", "")).upper(), "other")
    name = run.get("name") or ""
    line: dict[str, Any] = {
        "run": run.get("trace_id", ""),
        "span": run.get("id", ""),
        "parent": run.get("parent_run_id") or "",
        "kind": kind,
        "name": name,
        "tokens_in": _int(run.get("prompt_tokens")),
        "tokens_out": _int(run.get("completion_tokens")),
        "status": "error" if run.get("status") == "error" else "ok",
        "model": metadata.get("ls_model_name") or "",
        "provider": metadata.get("ls_provider") or "",
        "attrs": {},
    }
    if kind == "tool":
        line["tool"] = name
    if run.get("start_time"):
        line["start"] = run["start_time"]
    if run.get("end_time"):
        line["end"] = run["end_time"]
    contents = _contents(kind, run)
    if contents:
        line["contents"] = contents
    return line


def _int(value: Any) -> int:
    try:
        return int(value)
    except (TypeError, ValueError):
        return 0


def _contents(kind: str, run: dict[str, Any]) -> list[dict[str, str]]:
    # LangChain's inputs/outputs shape varies by run type (raw prompts, chat
    # message lists, chain variable maps); dumping the map as-is keeps every
    # run type's content readable without hand-parsing each one's schema.
    # capybara's analyzers (improvise, faithfulness) read an llm span's answer
    # off role "assistant"; everything else is tool-shaped "input"/"output".
    in_role, out_role = ("user", "assistant") if kind == "llm" else ("input", "output")
    contents = []
    for role, key in ((in_role, "inputs"), (out_role, "outputs")):
        body = run.get(key)
        if not body:
            continue
        text = body if isinstance(body, str) else json.dumps(body, ensure_ascii=False)
        contents.append({"role": role, "body": text})
    return contents


if __name__ == "__main__":
    main()
