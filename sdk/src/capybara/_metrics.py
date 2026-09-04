"""Process resource sampling, so a run that dies mid-call still says where.

A span reaches capybara only when it ends, so a process killed inside one
exports nothing for it. A gauge is read on its own timer instead, and the gap
where the readings stop is what marks the death.
"""

from __future__ import annotations

import itertools
import os
import sys
import threading
import time
from collections.abc import Iterable
from typing import TYPE_CHECKING

from opentelemetry.metrics import CallbackOptions, Observation
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.trace import SpanProcessor

from ._gpu import GPUReader

try:
    import resource
except ImportError:  # windows
    resource = None  # type: ignore[assignment]

if TYPE_CHECKING:
    from opentelemetry.context import Context
    from opentelemetry.sdk.trace import ReadableSpan, Span

TRACE_ID_ATTR = "capybara.trace_id"
SPAN_ID_ATTR = "capybara.span_id"
# The span a crash interrupts is never exported, so its name would be lost with
# it. Carrying it on the reading is the only way the live view can label a node
# that has not finished.
SPAN_NAME_ATTR = "capybara.span_name"

CPU_METRIC = "process.cpu.utilization"
RSS_METRIC = "process.memory.usage"
# Not semconv: OTel has no stable gpu convention, so these are capybara's own.
GPU_UTIL_METRIC = "capybara.gpu.utilization"
GPU_MEM_METRIC = "capybara.gpu.memory.usage"

# One second: long enough that the readings cost nothing on a local socket,
# short enough to leave several samples inside a step that runs for a few
# seconds. Not tuned against real traffic yet, unlike the cost-spike floor.
INTERVAL_MS = 1000


class ActiveSpans(SpanProcessor):
    """Tracks open spans so a gauge callback can name the one running.

    The callback runs on the exporter's own thread, where the OTel context is
    empty, so the current span cannot be read from it. Attribution is to the
    most recently started span still open: for a sequential pipeline that is
    the innermost node, and for concurrent tasks it is an approximation, since
    one process-wide reading cannot honestly be split between them.
    """

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._open: dict[int, tuple[int, str, str, str]] = {}
        self._seq = itertools.count()

    def on_start(self, span: Span, parent_context: Context | None = None) -> None:
        ctx = span.get_span_context()
        with self._lock:
            self._open[id(span)] = (
                next(self._seq),
                format(ctx.trace_id, "032x"),
                format(ctx.span_id, "016x"),
                span.name,
            )

    def on_end(self, span: ReadableSpan) -> None:
        with self._lock:
            self._open.pop(id(span), None)

    def newest(self) -> tuple[str, str, str] | None:
        with self._lock:
            if not self._open:
                return None
            _, trace_id, span_id, name = max(self._open.values())
        return trace_id, span_id, name


def _rss_bytes() -> int | None:
    """Current resident set, falling back to the peak where /proc is absent."""
    try:
        with open("/proc/self/statm") as fh:
            pages = int(fh.read().split()[1])
        return pages * os.sysconf("SC_PAGE_SIZE")
    except (OSError, IndexError, ValueError):
        pass
    if resource is None:
        return None
    # The high-water mark, which never falls back after a release. Still the
    # shape that matters before an out-of-memory kill, so it is reported.
    peak = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    # Linux counts kilobytes here; the BSDs and macOS count bytes.
    return peak if sys.platform == "darwin" else peak * 1024


class _Sampler:
    def __init__(self, active: ActiveSpans, gpu: GPUReader | None = None) -> None:
        self._active = active
        self._gpu = gpu
        self._cpu_at = time.process_time()
        self._wall_at = time.monotonic()

    def cpu(self, options: CallbackOptions) -> Iterable[Observation]:
        del options
        now_cpu, now_wall = time.process_time(), time.monotonic()
        elapsed, used = now_wall - self._wall_at, now_cpu - self._cpu_at
        self._cpu_at, self._wall_at = now_cpu, now_wall
        if elapsed <= 0:
            return []
        return self._observe(used / elapsed)

    def rss(self, options: CallbackOptions) -> Iterable[Observation]:
        del options
        rss = _rss_bytes()
        return [] if rss is None else self._observe(rss)

    def gpu_util(self, options: CallbackOptions) -> Iterable[Observation]:
        del options
        reading = self._gpu.reading() if self._gpu else None
        return [] if reading is None else self._observe(reading[0])

    def gpu_memory(self, options: CallbackOptions) -> Iterable[Observation]:
        del options
        reading = self._gpu.reading() if self._gpu else None
        return [] if reading is None else self._observe(reading[1])

    def _observe(self, value: float) -> list[Observation]:
        ids = self._active.newest()
        if ids is None:
            return []
        trace_id, span_id, name = ids
        return [
            Observation(
                value,
                {
                    TRACE_ID_ATTR: trace_id,
                    SPAN_ID_ATTR: span_id,
                    SPAN_NAME_ATTR: name,
                },
            )
        ]


def add_gauges(
    provider: MeterProvider, active: ActiveSpans, gpu: GPUReader | None = None
) -> None:
    """Register the process gauges, plus the gpu pair when a card was found."""
    meter = provider.get_meter("capybara")
    sampler = _Sampler(active, gpu)
    meter.create_observable_gauge(
        CPU_METRIC,
        callbacks=[sampler.cpu],
        unit="1",
        description="process cpu time over wall time since the last reading",
    )
    meter.create_observable_gauge(
        RSS_METRIC,
        callbacks=[sampler.rss],
        unit="By",
        description="process resident set size",
    )
    if gpu is None:
        return
    meter.create_observable_gauge(
        GPU_UTIL_METRIC,
        callbacks=[sampler.gpu_util],
        unit="1",
        description="gpu utilization, whole device rather than this process",
    )
    meter.create_observable_gauge(
        GPU_MEM_METRIC,
        callbacks=[sampler.gpu_memory],
        unit="By",
        description="gpu memory in use, whole device",
    )
