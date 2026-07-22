"""Tool output schema registry: Pydantic models serialized onto spans."""

from __future__ import annotations

import json
from typing import TYPE_CHECKING

from opentelemetry.sdk.trace import SpanProcessor

if TYPE_CHECKING:
    from opentelemetry.context import Context
    from opentelemetry.sdk.trace import Span
    from pydantic import BaseModel

SCHEMA_ATTR = "capybara.schema"
_TOOL_NAME_ATTRS = ("gen_ai.tool.name", "mcp.tool.name")

_registry: dict[str, str] = {}


def schema(tool_name: str, model: type[BaseModel]) -> type[BaseModel]:
    """Register a tool's Pydantic output model; attached to matching tool spans."""
    _registry[tool_name] = json.dumps(model.model_json_schema(), separators=(",", ":"))
    return model


def schema_for(tool_name: str) -> str | None:
    return _registry.get(tool_name)


# SchemaSpanProcessor attaches declared schemas to third-party tool spans whose
# tool name is already set at span start; capybara.trace attaches its own.
class SchemaSpanProcessor(SpanProcessor):
    def on_start(self, span: Span, parent_context: Context | None = None) -> None:
        attributes = span.attributes or {}
        for key in _TOOL_NAME_ATTRS:
            name = attributes.get(key)
            if isinstance(name, str) and name in _registry:
                span.set_attribute(SCHEMA_ATTR, _registry[name])
                return
