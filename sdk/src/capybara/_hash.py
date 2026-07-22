"""Request hashing, mirrored by internal/replay/hash.go; both must agree."""

from __future__ import annotations

import hashlib
import json
from typing import Any


def hash_llm_request(model: str, messages: list[tuple[str, str]]) -> str:
    payload = [model, [[role, text] for role, text in messages]]
    return _sum(json.dumps(payload, separators=(",", ":"), ensure_ascii=False))


def hash_tool_call(tool: str, arguments: str) -> str:
    return _sum(tool + "\x00" + arguments)


def message_text(body: str) -> str:
    """Reduce a recorded message body to the plain text a live call carries."""
    try:
        value = json.loads(body)
    except ValueError:
        return body.strip()
    return _text(value).strip()


def value_text(value: Any) -> str:
    """Reduce a live message content, which may be blocks, to plain text."""
    if isinstance(value, str):
        return value.strip()
    return _text(value).strip()


def _text(value: Any) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        return "".join(_text(element) for element in value)
    if isinstance(value, dict):
        for key in ("content", "text"):
            if key in value:
                return _text(value[key])
    return ""


def _sum(payload: str) -> str:
    return hashlib.sha256(payload.encode("utf8")).hexdigest()
