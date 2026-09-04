"""Three nodes with different resource shapes, the last one killed.

Recorded by demo/monitor.tape. Each node is written to look like something
distinct on the graphs: one burns cpu, one grows the heap, and the third dies
inside its span the way an out-of-memory kill does, leaving nothing to export.
"""

import os
import signal
import time

import capybara

capybara.init(service_name="rag-pipeline")


@capybara.trace(tool="parse_corpus")
def parse_corpus(chunks: int) -> dict:
    end = time.monotonic() + 4
    total = 0
    while time.monotonic() < end:
        total += sum(i * i for i in range(2000))
    return {"chunks": chunks, "checksum": total % 1000}


@capybara.trace(tool="embed_corpus")
def embed_corpus(parsed: dict) -> dict:
    held = []
    for _ in range(5):
        held.append(bytearray(60 * 1024 * 1024))
        time.sleep(0.8)
    return {"vectors": len(held)}


@capybara.trace(tool="build_index")
def build_index(vectors: dict) -> dict:
    time.sleep(3)
    os.kill(os.getpid(), signal.SIGKILL)
    return {}


@capybara.trace(kind="agent", name="rag_pipeline")
def rag_pipeline() -> None:
    build_index(embed_corpus(parse_corpus(128)))


rag_pipeline()
