"""A real LangChain agent, instrumented by OpenLLMetry, exporting to capybara.

The tool genuinely fails; the next model turn answers with a price it never
received. Nothing about the trace is hand-written.
"""

import os

os.environ.setdefault("OPENAI_API_KEY", "sk-not-a-real-key")

from langchain_core.tools import tool  # noqa: E402
from langchain_openai import ChatOpenAI  # noqa: E402
from langgraph.prebuilt import ToolNode  # noqa: E402
from langgraph.prebuilt import create_react_agent as create_agent  # noqa: E402
from opentelemetry import trace  # noqa: E402
from opentelemetry.exporter.otlp.proto.http.trace_exporter import (
    OTLPSpanExporter,  # noqa: E402
)
from opentelemetry.instrumentation.langchain import LangchainInstrumentor  # noqa: E402
from opentelemetry.sdk.resources import Resource  # noqa: E402
from opentelemetry.sdk.trace import TracerProvider  # noqa: E402
from opentelemetry.sdk.trace.export import BatchSpanProcessor  # noqa: E402

provider = TracerProvider(
    resource=Resource.create({"service.name": "langchain-caught"})
)
provider.add_span_processor(
    BatchSpanProcessor(OTLPSpanExporter(endpoint="http://127.0.0.1:4318/v1/traces"))
)
trace.set_tracer_provider(provider)
LangchainInstrumentor().instrument(tracer_provider=provider)


@tool
def get_stock_price(symbol: str) -> str:
    """Look up the latest close for a ticker."""
    # The upstream quote service is down. The tool reports that rather than
    # inventing a number - which is exactly what the model then does.
    raise RuntimeError("upstream quote service unavailable (502)")


def main():
    model = ChatOpenAI(
        model="gpt-4o-mini",
        base_url="http://127.0.0.1:8099/v1",
        temperature=0,
    )
    # What a production agent does: the framework catches the failure and hands
    # it back to the model, which then keeps going. The tool span is marked
    # failed by the instrumentor, which is the signal capybara reads.
    agent = create_agent(model, ToolNode([get_stock_price], handle_tool_errors=True))
    result = agent.invoke(
        {"messages": [("user", "What are my 120 NVDA shares worth at the last close?")]}
    )
    print("final:", result["messages"][-1].content)
    provider.shutdown()


if __name__ == "__main__":
    main()
