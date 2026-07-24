"""A hand-rolled OpenAI tool loop, instrumented by OpenInference.

Same failure as the LangChain run, through a different client and a different
attribute convention, so the two can be compared side by side in capybara.
"""

import json
import os

os.environ.setdefault("OPENAI_API_KEY", "sk-not-a-real-key")

from openai import OpenAI  # noqa: E402
from openinference.instrumentation.openai import OpenAIInstrumentor  # noqa: E402
from opentelemetry import trace  # noqa: E402
from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
    OTLPSpanExporter,  # noqa: E402
)
from opentelemetry.sdk.resources import Resource  # noqa: E402
from opentelemetry.sdk.trace import TracerProvider  # noqa: E402
from opentelemetry.sdk.trace.export import BatchSpanProcessor  # noqa: E402

provider = TracerProvider(
    resource=Resource.create({"service.name": "openai-portfolio"})
)
provider.add_span_processor(
    BatchSpanProcessor(OTLPSpanExporter(endpoint="http://127.0.0.1:4318/v1/traces"))
)
trace.set_tracer_provider(provider)
OpenAIInstrumentor().instrument(tracer_provider=provider)
tracer = trace.get_tracer("portfolio-agent")

TOOLS = [
    {
        "type": "function",
        "function": {
            "name": "get_stock_price",
            "description": "Look up the latest close for a ticker.",
            "parameters": {
                "type": "object",
                "properties": {"symbol": {"type": "string"}},
                "required": ["symbol"],
            },
        },
    }
]


def get_stock_price(symbol: str) -> str:
    # The quote service is down, and the tool says so rather than guessing.
    return json.dumps({"status": 502, "error": "upstream quote service unavailable"})


def main():
    client = OpenAI(base_url="http://127.0.0.1:8099/v1")
    messages = [
        {
            "role": "user",
            "content": "What are my 120 NVDA shares worth at the last close?",
        }
    ]
    with tracer.start_as_current_span("portfolio agent") as root:
        root.set_attribute("openinference.span.kind", "AGENT")
        for _ in range(4):
            reply = (
                client.chat.completions.create(
                    model="gpt-4o-mini", messages=messages, tools=TOOLS
                )
                .choices[0]
                .message
            )
            messages.append(reply.model_dump(exclude_none=True))
            if not reply.tool_calls:
                print("final:", reply.content)
                break
            for call in reply.tool_calls:
                args = json.loads(call.function.arguments)
                with tracer.start_as_current_span(call.function.name) as span:
                    span.set_attribute("openinference.span.kind", "TOOL")
                    span.set_attribute("tool.name", call.function.name)
                    span.set_attribute("input.value", call.function.arguments)
                    result = get_stock_price(**args)
                    span.set_attribute("output.value", result)
                messages.append(
                    {"role": "tool", "tool_call_id": call.id, "content": result}
                )
    provider.shutdown()


if __name__ == "__main__":
    main()
