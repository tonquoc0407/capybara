"""OTel bootstrap: a provider plus an OTLP exporter to a local capybara."""

from __future__ import annotations

import json
import os
import sys
from typing import TYPE_CHECKING

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import SpanProcessor, TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from ._schema import SchemaSpanProcessor

if TYPE_CHECKING:
    from opentelemetry.context import Context
    from opentelemetry.sdk.trace import Span

DEFAULT_ENDPOINT = "http://127.0.0.1:4318/v1/traces"
ENTRYPOINT_ATTR = "capybara.entrypoint"
CWD_ATTR = "capybara.cwd"

_configured = False


# Time-travel re-executes the recorded process, so the root span carries how it
# was launched. Resource attributes would not survive capybara's span mapping.
class EntrypointSpanProcessor(SpanProcessor):
    def on_start(self, span: Span, parent_context: Context | None = None) -> None:
        if span.parent is not None:
            return
        span.set_attribute(ENTRYPOINT_ATTR, json.dumps([sys.executable, *sys.argv]))
        span.set_attribute(CWD_ATTR, os.getcwd())


def _resolved_endpoint(endpoint: str | None) -> str | None:
    """None lets the OTel exporter resolve OTEL_EXPORTER_OTLP_* itself.

    Only fall back to a local capybara when nothing else is configured, so
    moving the collector with `capybara -otlp` needs no code change here.
    """
    if endpoint is not None:
        return endpoint
    for var in ("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT"):
        if os.getenv(var):
            return None
    return DEFAULT_ENDPOINT


def init(
    *,
    service_name: str = "capybara",
    endpoint: str | None = None,
) -> TracerProvider:
    """Export spans to a local capybara, reusing any provider already installed."""
    global _configured
    provider = trace.get_tracer_provider()
    if not isinstance(provider, TracerProvider):
        provider = TracerProvider(
            resource=Resource.create({"service.name": service_name})
        )
        trace.set_tracer_provider(provider)
    if not _configured:
        provider.add_span_processor(EntrypointSpanProcessor())
        provider.add_span_processor(SchemaSpanProcessor())
        provider.add_span_processor(
            BatchSpanProcessor(OTLPSpanExporter(endpoint=_resolved_endpoint(endpoint)))
        )
        _configured = True
    return provider
