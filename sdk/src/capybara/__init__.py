"""capybara-sdk: emit OpenTelemetry traces to a local capybara trace debugger."""

from ._otel import init
from ._schema import schema
from ._trace import trace

__all__ = ["init", "schema", "trace"]
__version__ = "0.1.0"
