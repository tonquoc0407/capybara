"""Run KnightVision's own chess agent under capybara, with the real model.

Nothing here changes the agent: it imports build_agent as the project defines
it, turns on the OpenLLMetry instrumentor, and asks one question. Whether the
model improvises past a failing tool is the model's decision, not a script's.
"""

import os
import sys

sys.path.insert(0, os.environ["KNIGHTVISION"])

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.langchain import LangchainInstrumentor
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

provider = TracerProvider(
    resource=Resource.create({"service.name": "knightvision-chess-agent"})
)
provider.add_span_processor(
    BatchSpanProcessor(OTLPSpanExporter(endpoint="http://127.0.0.1:4318/v1/traces"))
)
trace.set_tracer_provider(provider)
LangchainInstrumentor().instrument(tracer_provider=provider)

from ml.chess_agent.agent import build_agent  # noqa: E402

DB = os.path.join(os.environ["KNIGHTVISION"], "warehouse", "knightvision.duckdb")


def main():
    question = sys.argv[1]
    agent = build_agent(DB)
    result = agent.invoke({"input": question})
    print("ANSWER:", result["output"])
    for step, observation in result.get("intermediate_steps", []):
        print(f"TOOL {step.tool}: {str(observation)[:200]}")
    provider.shutdown()


if __name__ == "__main__":
    main()
