#!/usr/bin/env python3
"""Fetch a Weights & Biases Weave trace and emit capybara's import jsonl."""

from __future__ import annotations

import argparse
import base64
import json
import os
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any


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
            for call in _calls(cfg, args.project, trace_id):
                out.write(
                    json.dumps(_span_line(trace_id, call), ensure_ascii=False) + "\n"
                )
                n += 1
            if n == 0:
                print(f"warning: no calls for trace {trace_id}", file=sys.stderr)
    finally:
        if out is not sys.stdout:
            out.close()


def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("project", help="Weave project identifier (<entity>/<project>)")
    p.add_argument("trace_id", nargs="+", help="trace id(s) to export")
    p.add_argument(
        "--host", default=os.environ.get("WEAVE_HOST", "https://trace.wandb.ai")
    )
    p.add_argument("--api-key", default=os.environ.get("WANDB_API_KEY"))
    p.add_argument("--out", default="-", help="output path, or - for stdout")
    return p.parse_args()


def _auth_header(api_key: str) -> str:
    if api_key.startswith("Bearer "):
        return api_key
    creds = base64.b64encode(f"api:{api_key}".encode()).decode("ascii")
    return f"Basic {creds}"


def _calls(cfg: Config, project: str, trace_id: str):
    payload = {
        "project_id": project,
        "filter": {"trace_ids": [trace_id]},
    }
    body = _post(cfg, "/call/query", payload)
    if isinstance(body, list):
        yield from body
    elif isinstance(body, dict):
        yield from body.get("calls", [])


def _post(cfg: Config, path: str, payload: dict[str, Any]) -> Any:
    url = f"{cfg.host}{path}"
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    if cfg.api_key:
        req.add_header("Authorization", _auth_header(cfg.api_key))
    try:
        with urllib.request.urlopen(req) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        raise SystemExit(f"weave {path}: {e.code} {e.reason}") from e


def _map_kind(op_name: str, inputs: dict[str, Any]) -> str:
    name_lower = op_name.lower()
    if any(
        k in name_lower
        for k in ("chat", "completion", "generate", "llm", "predict", "message")
    ):
        return "llm"
    if any(k in name_lower for k in ("tool", "function")):
        return "tool"
    if any(k in name_lower for k in ("retriev", "search", "embed", "vector")):
        return "retrieval"
    if any(
        k in name_lower for k in ("agent", "chain", "pipeline", "graph", "workflow")
    ):
        return "agent"
    if "messages" in inputs or "prompt" in inputs:
        return "llm"
    return "other"


def _span_line(trace_id: str, call: dict[str, Any]) -> dict[str, Any]:
    call_id = call.get("id") or call.get("call_id", "")
    parent_id = call.get("parent_id") or ""
    op_name = call.get("op_name") or call.get("name") or ""
    inputs = call.get("inputs") or {}
    output = call.get("output")
    summary = call.get("summary") or {}
    attributes = call.get("attributes") or {}

    kind = _map_kind(op_name, inputs)
    if not parent_id and kind == "other":
        kind = "agent"

    tokens_in = 0
    tokens_out = 0
    usage = summary.get("usage") or summary
    if isinstance(usage, dict):
        for k in ("prompt_tokens", "input_tokens", "prompt_token_count"):
            if k in usage:
                tokens_in = _int(usage[k])
                break
        for k in ("completion_tokens", "output_tokens", "candidates_token_count"):
            if k in usage:
                tokens_out = _int(usage[k])
                break
        if tokens_in == 0 and tokens_out == 0:
            for sub in usage.values():
                if isinstance(sub, dict):
                    tokens_in = _int(
                        sub.get("prompt_tokens") or sub.get("input_tokens")
                    )
                    tokens_out = _int(
                        sub.get("completion_tokens") or sub.get("output_tokens")
                    )
                    if tokens_in or tokens_out:
                        break

    model = str(
        summary.get("model") or attributes.get("model") or inputs.get("model") or ""
    )
    provider = ""
    name_lower = op_name.lower()
    model_lower = model.lower()
    if "openai" in name_lower or "gpt" in model_lower:
        provider = "openai"
    elif "anthropic" in name_lower or "claude" in model_lower:
        provider = "anthropic"
    elif "google" in name_lower or "gemini" in model_lower:
        provider = "google"

    status = "error" if call.get("exception") else "ok"

    line: dict[str, Any] = {
        "run": trace_id,
        "span": call_id,
        "parent": parent_id,
        "kind": kind,
        "name": op_name,
        "tokens_in": tokens_in,
        "tokens_out": tokens_out,
        "status": status,
        "model": model,
        "provider": provider,
        "attrs": {},
    }
    if kind == "tool":
        line["tool"] = attributes.get("tool.name") or op_name
    if call.get("started_at"):
        line["start"] = call["started_at"]
    if call.get("ended_at"):
        line["end"] = call["ended_at"]

    contents = _contents(kind, inputs, output)
    if contents:
        line["contents"] = contents
    return line


def _int(value: Any) -> int:
    try:
        return int(value)
    except (TypeError, ValueError):
        return 0


def _contents(kind: str, inputs: dict[str, Any], output: Any) -> list[dict[str, str]]:
    contents = []
    if kind == "llm":
        if "messages" in inputs:
            msgs = inputs["messages"]
            contents.append(
                {
                    "role": "user",
                    "body": msgs
                    if isinstance(msgs, str)
                    else json.dumps(msgs, ensure_ascii=False),
                }
            )
        elif "prompt" in inputs:
            p = inputs["prompt"]
            contents.append(
                {
                    "role": "user",
                    "body": p
                    if isinstance(p, str)
                    else json.dumps(p, ensure_ascii=False),
                }
            )
        elif inputs:
            contents.append(
                {
                    "role": "user",
                    "body": json.dumps(inputs, ensure_ascii=False),
                }
            )
        if output is not None:
            contents.append(
                {
                    "role": "assistant",
                    "body": output
                    if isinstance(output, str)
                    else json.dumps(output, ensure_ascii=False),
                }
            )
    else:
        if inputs:
            contents.append(
                {
                    "role": "input",
                    "body": json.dumps(inputs, ensure_ascii=False),
                }
            )
        if output is not None:
            contents.append(
                {
                    "role": "output",
                    "body": output
                    if isinstance(output, str)
                    else json.dumps(output, ensure_ascii=False),
                }
            )
    return contents


if __name__ == "__main__":
    main()
