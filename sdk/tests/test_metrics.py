from types import SimpleNamespace

import capybara._metrics as metrics
import capybara._otel as otel
import pytest
from opentelemetry.sdk.trace import TracerProvider


class Started:
    """Records whether init decided to sample, without opening a socket."""

    def __init__(self) -> None:
        self.endpoints: list[str | None] = []

    def __call__(self, service_name: str, traces_endpoint: str | None) -> None:
        self.endpoints.append(traces_endpoint)


def init_with(monkeypatch, **kwargs) -> Started:
    started = Started()
    monkeypatch.setattr(otel, "_start_metrics", started)
    monkeypatch.setattr(otel.trace, "get_tracer_provider", lambda: TracerProvider())
    otel._configured = False
    otel.init(**kwargs)
    return started


def test_metrics_default_on_for_a_local_capybara(monkeypatch) -> None:
    monkeypatch.delenv("OTEL_EXPORTER_OTLP_ENDPOINT", raising=False)
    monkeypatch.delenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", raising=False)
    assert init_with(monkeypatch).endpoints == [otel.DEFAULT_ENDPOINT]


# The readings carry a span id per data point. That cardinality belongs in a
# local debugger, not in whatever backend someone pointed OTel at.
def test_metrics_default_off_for_a_foreign_collector(monkeypatch) -> None:
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector.internal:4318")
    assert init_with(monkeypatch).endpoints == []


def test_metrics_can_be_forced_on_for_a_foreign_collector(monkeypatch) -> None:
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector.internal:4318")
    assert init_with(monkeypatch, metrics=True).endpoints == [None]


def test_metrics_can_be_turned_off_locally(monkeypatch) -> None:
    monkeypatch.delenv("OTEL_EXPORTER_OTLP_ENDPOINT", raising=False)
    monkeypatch.delenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", raising=False)
    assert init_with(monkeypatch, metrics=False).endpoints == []


def test_metrics_endpoint_swaps_the_signal_path() -> None:
    assert (
        otel._metrics_endpoint("http://127.0.0.1:4318/v1/traces")
        == "http://127.0.0.1:4318/v1/metrics"
    )
    assert otel._metrics_endpoint(None) is None


class FakeSpan:
    def __init__(self, trace_id: int, span_id: int, name: str = "node") -> None:
        self._ctx = type("Ctx", (), {"trace_id": trace_id, "span_id": span_id})()
        self.name = name

    def get_span_context(self):
        return self._ctx


# A gauge callback runs on the exporter's thread, where the OTel context is
# empty, so the active span has to be tracked rather than read from context.
def test_active_spans_names_the_innermost_open_span() -> None:
    active = metrics.ActiveSpans()
    outer, inner = FakeSpan(1, 10, "outer"), FakeSpan(1, 20, "inner")
    active.on_start(outer)
    active.on_start(inner)
    trace_id, span_id, name = active.newest()
    assert trace_id == format(1, "032x")
    assert span_id == format(20, "016x")
    assert name == "inner"


def test_active_spans_falls_back_to_the_parent_when_the_child_ends() -> None:
    active = metrics.ActiveSpans()
    outer, inner = FakeSpan(1, 10, "outer"), FakeSpan(1, 20, "inner")
    active.on_start(outer)
    active.on_start(inner)
    active.on_end(inner)
    assert active.newest() == (format(1, "032x"), format(10, "016x"), "outer")


def test_active_spans_reports_nothing_when_idle() -> None:
    active = metrics.ActiveSpans()
    span = FakeSpan(1, 10)
    active.on_start(span)
    active.on_end(span)
    assert active.newest() is None


def test_sampler_drops_readings_taken_outside_any_span() -> None:
    sampler = metrics._Sampler(metrics.ActiveSpans())
    assert list(sampler.rss(None)) == []


def test_sampler_attributes_a_reading_to_the_open_span() -> None:
    active = metrics.ActiveSpans()
    active.on_start(FakeSpan(7, 9, "embed_corpus"))
    sampler = metrics._Sampler(active)
    observations = list(sampler.rss(None))
    assert len(observations) == 1
    attrs = observations[0].attributes
    assert attrs[metrics.TRACE_ID_ATTR] == format(7, "032x")
    assert attrs[metrics.SPAN_ID_ATTR] == format(9, "016x")
    # The span a crash interrupts never arrives, so the reading carries its name.
    assert attrs[metrics.SPAN_NAME_ATTR] == "embed_corpus"
    assert observations[0].value > 0


def test_rss_reads_the_live_process() -> None:
    rss = metrics._rss_bytes()
    assert rss is not None and rss > 0


@pytest.mark.parametrize(
    "error",
    [
        metrics.psutil.AccessDenied(),
        metrics.psutil.NoSuchProcess(1),
        metrics.psutil.ZombieProcess(1),
        OSError("memory unavailable"),
    ],
)
def test_rss_returns_none_when_memory_is_unavailable(monkeypatch, error) -> None:
    class Denied:
        pid = metrics.os.getpid()

        def memory_info(self):
            raise error

    monkeypatch.setattr(metrics, "_PROCESS", Denied())
    assert metrics._rss_bytes() is None


def test_rss_reuses_the_handle_but_reads_current_memory(monkeypatch) -> None:
    values = iter([4096, 2048])
    created = []

    class Process:
        def __init__(self, pid):
            self.pid = pid
            created.append(pid)

        def memory_info(self):
            return SimpleNamespace(rss=next(values))

    monkeypatch.setattr(metrics, "_PROCESS", None)
    monkeypatch.setattr(metrics.psutil, "Process", Process)
    assert metrics._rss_bytes() == 4096
    assert metrics._rss_bytes() == 2048
    assert created == [metrics.os.getpid()]


def test_rss_refreshes_an_inherited_process_handle(monkeypatch) -> None:
    created = []

    def process(pid):
        created.append(pid)
        return SimpleNamespace(pid=pid, memory_info=lambda: SimpleNamespace(rss=2048))

    monkeypatch.setattr(metrics, "_PROCESS", SimpleNamespace(pid=-1))
    monkeypatch.setattr(metrics.psutil, "Process", process)
    assert metrics._rss_bytes() == 2048
    assert created == [metrics.os.getpid()]


def test_rss_handle_creation_failure_is_graceful(monkeypatch) -> None:
    def denied(pid):
        raise metrics.psutil.AccessDenied(pid)

    monkeypatch.setattr(metrics, "_PROCESS", None)
    monkeypatch.setattr(metrics.psutil, "Process", denied)
    assert metrics._rss_bytes() is None


def test_sampler_omits_unavailable_memory(monkeypatch) -> None:
    active = metrics.ActiveSpans()
    active.on_start(FakeSpan(7, 9))
    monkeypatch.setattr(metrics, "_rss_bytes", lambda: None)
    assert list(metrics._Sampler(active).rss(None)) == []


class FakeGPU:
    def __init__(self, reading):
        self._reading = reading

    def reading(self):
        return self._reading


def test_gpu_gauges_report_when_a_card_is_present() -> None:
    active = metrics.ActiveSpans()
    active.on_start(FakeSpan(1, 2, "train"))
    sampler = metrics._Sampler(active, FakeGPU((0.42, 1024)))
    assert list(sampler.gpu_util(None))[0].value == 0.42
    assert list(sampler.gpu_memory(None))[0].value == 1024


def test_gpu_gauges_stay_quiet_without_a_card() -> None:
    active = metrics.ActiveSpans()
    active.on_start(FakeSpan(1, 2))
    sampler = metrics._Sampler(active, None)
    assert list(sampler.gpu_util(None)) == []
    assert list(sampler.gpu_memory(None)) == []


def test_gpu_gauges_are_not_registered_without_a_card(monkeypatch) -> None:
    registered: list[str] = []

    class FakeMeter:
        def create_observable_gauge(self, name, **kwargs):
            registered.append(name)

    class FakeProvider:
        def get_meter(self, _name):
            return FakeMeter()

    metrics.add_gauges(FakeProvider(), metrics.ActiveSpans(), None)
    assert metrics.GPU_UTIL_METRIC not in registered
    metrics.add_gauges(FakeProvider(), metrics.ActiveSpans(), FakeGPU((0.1, 2)))
    assert metrics.GPU_UTIL_METRIC in registered
