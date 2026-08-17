#!/usr/bin/env python3
"""Fetch a Langfuse trace via its Observations API and emit capybara's import jsonl."""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from base64 import b64encode
from dataclasses import dataclass
from typing import Any
from urllib.parse import urlencode

_PAGE_LIMIT = 100

# v1 stops serving 2026-11-16 in favor of v2's cursor pagination and
# field-group selection; request every group this script reads (v2 only
# returns core+basic unless asked).
_FIELD_GROUPS = "core,basic,io,usage,model,metrics"

_KIND_BY_TYPE = {
    "AGENT": "agent",
    "TOOL": "tool",
    "RETRIEVER": "retrieval",
    "GENERATION": "llm",
    "EMBEDDING": "llm",
}


@dataclass
class Config:
    host: str
    public_key: str
    secret_key: str


def main() -> None:
    args = _parse_args()
    cfg = Config(
        host=args.host.rstrip("/"),
        public_key=args.public_key or _require_env("LANGFUSE_PUBLIC_KEY"),
        secret_key=args.secret_key or _require_env("LANGFUSE_SECRET_KEY"),
    )
    out = sys.stdout if args.out == "-" else open(args.out, "w", encoding="utf8")
    try:
        for trace_id in args.trace_id:
            n = 0
            for obs in _observations(cfg, trace_id):
                out.write(
                    json.dumps(_span_line(trace_id, obs), ensure_ascii=False) + "\n"
                )
                n += 1
            if n == 0:
                print(f"warning: no observations for trace {trace_id}", file=sys.stderr)
    finally:
        if out is not sys.stdout:
            out.close()


def _parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("trace_id", nargs="+", help="Langfuse trace id(s) to export")
    p.add_argument(
        "--host", default=os.environ.get("LANGFUSE_HOST", "https://cloud.langfuse.com")
    )
    p.add_argument("--public-key", default=os.environ.get("LANGFUSE_PUBLIC_KEY"))
    p.add_argument("--secret-key", default=os.environ.get("LANGFUSE_SECRET_KEY"))
    p.add_argument("--out", default="-", help="output path, or - for stdout")
    return p.parse_args()


def _require_env(name: str) -> str:
    value = os.environ.get(name)
    if not value:
        raise SystemExit(f"{name} not set and no matching flag given")
    return value


def _observations(cfg: Config, trace_id: str):
    cursor = None
    while True:
        params = {"traceId": trace_id, "limit": _PAGE_LIMIT, "fields": _FIELD_GROUPS}
        if cursor:
            params["cursor"] = cursor
        body = _get(cfg, "/api/public/v2/observations", params)
        items = body.get("data", [])
        if not items:
            return
        yield from items
        cursor = (body.get("meta") or {}).get("cursor")
        if not cursor:
            return


def _get(cfg: Config, path: str, params: dict[str, Any]) -> dict[str, Any]:
    url = f"{cfg.host}{path}?{urlencode(params)}"
    req = urllib.request.Request(url)
    token = b64encode(f"{cfg.public_key}:{cfg.secret_key}".encode()).decode()
    req.add_header("Authorization", f"Basic {token}")
    try:
        with urllib.request.urlopen(req) as resp:
            return json.load(resp)
    except urllib.error.HTTPError as e:
        raise SystemExit(f"langfuse {path}: {e.code} {e.reason}") from e


def _span_line(trace_id: str, obs: dict[str, Any]) -> dict[str, Any]:
    kind = _KIND_BY_TYPE.get(str(obs.get("type", "")).upper(), "other")
    usage = obs.get("usageDetails") or {}
    name = obs.get("name") or str(obs.get("type", "")).lower()
    line: dict[str, Any] = {
        "run": trace_id,
        "span": obs["id"],
        "parent": obs.get("parentObservationId") or "",
        "kind": kind,
        "name": name,
        "tokens_in": usage.get("input") or usage.get("inputTokens") or 0,
        "tokens_out": usage.get("output") or usage.get("outputTokens") or 0,
        "status": "error" if obs.get("level") == "ERROR" else "ok",
        "model": obs.get("providedModelName") or "",
        "attrs": _attrs(obs),
    }
    if kind == "tool":
        line["tool"] = name
    # time.Time on the capybara side leaves a missing key at its zero value;
    # an empty string would fail to parse, so start/end are omitted rather
    # than sent blank when Langfuse hasn't recorded one.
    if obs.get("startTime"):
        line["start"] = obs["startTime"]
    if obs.get("endTime"):
        line["end"] = obs["endTime"]
    contents = _contents(kind, obs)
    if contents:
        line["contents"] = contents
    return line


def _attrs(obs: dict[str, Any]) -> dict[str, Any]:
    attrs = {
        "environment": obs.get("environment"),
        "version": obs.get("version"),
        "status_message": obs.get("statusMessage"),
        "total_cost_usd": obs.get("totalCost"),
        "latency_s": obs.get("latency"),
    }
    return {k: v for k, v in attrs.items() if v not in (None, "")}


def _contents(kind: str, obs: dict[str, Any]) -> list[dict[str, str]]:
    # capybara's analyzers (improvise, faithfulness) read an llm span's answer
    # off role "assistant"; everything else is tool-shaped "input"/"output".
    in_role, out_role = ("user", "assistant") if kind == "llm" else ("input", "output")
    contents = []
    for role, key in ((in_role, "input"), (out_role, "output")):
        body = obs.get(key)
        if body in (None, ""):
            continue
        text = body if isinstance(body, str) else json.dumps(body, ensure_ascii=False)
        contents.append({"role": role, "body": text})
    return contents


if __name__ == "__main__":
    main()
