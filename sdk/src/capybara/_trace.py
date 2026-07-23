"""Span decorator for un-instrumented agent code."""

from __future__ import annotations

import functools
import inspect
import json
from collections.abc import Callable
from dataclasses import asdict, is_dataclass
from typing import TYPE_CHECKING, Any, TypeVar, cast, overload

from opentelemetry import trace as _otel_trace

from ._schema import SCHEMA_ATTR, schema_for

if TYPE_CHECKING:
    from opentelemetry.trace import Span

F = TypeVar("F", bound=Callable[..., Any])

_OPERATION = {"tool": "execute_tool", "agent": "invoke_agent", "llm": "chat"}

# capybara.replay installs a server here to return recorded outputs instead of
# executing tools, which may touch the network or the disk.
tool_server: Callable[[str, str], Any] | None = None


@overload
def trace(func: F) -> F: ...
@overload
def trace(
    *, name: str | None = ..., tool: str | None = ..., kind: str = ...
) -> Callable[[F], F]: ...


def trace(
    func: F | None = None,
    *,
    name: str | None = None,
    tool: str | None = None,
    kind: str = "tool",
) -> Any:
    """Wrap a function so each call is recorded as a capybara span."""

    def decorate(fn: F) -> F:
        # A tool span's name defaults to the wrapped function's name, so a
        # schema registered under that name is found without repeating it.
        tool_name = tool or (fn.__name__ if kind == "tool" else None)
        span_name = name or (f"execute_tool {tool_name}" if tool_name else fn.__name__)
        operation = _OPERATION.get(kind, kind)
        signature = _signature(fn)
        target = (
            None
            if fn.__module__ == "__main__"
            else f"{fn.__module__}:{fn.__qualname__}"
        )

        def annotate(
            span: Span, args: tuple[Any, ...], kwargs: dict[str, Any]
        ) -> str | None:
            span.set_attribute("gen_ai.operation.name", operation)
            if tool_name is None:
                return None
            arguments = _dump(_arguments(signature, args, kwargs))
            span.set_attribute("gen_ai.tool.name", tool_name)
            span.set_attribute("gen_ai.tool.call.arguments", arguments)
            # Lets an exported test import and call the real tool instead of
            # only replaying what it returned that day. A tool defined in the
            # script being run has no importable name, so it gets none.
            if target is not None:
                span.set_attribute("capybara.target", target)
            declared = schema_for(tool_name)
            if declared is not None:
                span.set_attribute(SCHEMA_ATTR, declared)
            return arguments

        def record(span: Span, result: Any) -> None:
            if tool_name is not None:
                span.set_attribute("gen_ai.tool.call.result", _dump(result))

        def recorded(arguments: str | None) -> tuple[bool, Any]:
            if tool_server is None or tool_name is None or arguments is None:
                return False, None
            return True, tool_server(tool_name, arguments)

        if inspect.iscoroutinefunction(fn):

            @functools.wraps(fn)
            async def awrapper(*args: Any, **kwargs: Any) -> Any:
                tracer = _otel_trace.get_tracer("capybara")
                with tracer.start_as_current_span(span_name) as span:
                    served, result = recorded(annotate(span, args, kwargs))
                    if not served:
                        result = await fn(*args, **kwargs)
                    record(span, result)
                    return result

            return cast(F, awrapper)

        @functools.wraps(fn)
        def wrapper(*args: Any, **kwargs: Any) -> Any:
            tracer = _otel_trace.get_tracer("capybara")
            with tracer.start_as_current_span(span_name) as span:
                served, result = recorded(annotate(span, args, kwargs))
                if not served:
                    result = fn(*args, **kwargs)
                record(span, result)
                return result

        return cast(F, wrapper)

    return decorate if func is None else decorate(func)


def _signature(fn: Callable[..., Any]) -> inspect.Signature | None:
    try:
        return inspect.signature(fn)
    except (TypeError, ValueError):
        return None


# Positional arguments are bound to their parameter names so capybara's schema
# inference sees the same field set the tool declares.
def _arguments(
    signature: inspect.Signature | None, args: tuple[Any, ...], kwargs: dict[str, Any]
) -> Any:
    if signature is not None:
        try:
            return dict(signature.bind_partial(*args, **kwargs).arguments)
        except TypeError:
            pass
    return {"args": list(args), "kwargs": kwargs}


def _dump(value: Any) -> str:
    return json.dumps(value, default=_fallback, separators=(",", ":"))


def _fallback(value: Any) -> Any:
    model_dump = getattr(value, "model_dump", None)
    if callable(model_dump):
        return model_dump()
    if is_dataclass(value) and not isinstance(value, type):
        return asdict(value)
    return str(value)
