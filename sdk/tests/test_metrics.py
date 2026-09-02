import capybara._metrics as metrics
import capybara._otel as otel
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
    def __init__(self, trace_id: int, span_id: int) -> None:
        self._ctx = type("Ctx", (), {"trace_id": trace_id, "span_id": span_id})()

    def get_span_context(self):
        return self._ctx


# A gauge callback runs on the exporter's thread, where the OTel context is
# empty, so the active span has to be tracked rather than read from context.
def test_active_spans_names_the_innermost_open_span() -> None:
    active = metrics.ActiveSpans()
    outer, inner = FakeSpan(1, 10), FakeSpan(1, 20)
    active.on_start(outer)
    active.on_start(inner)
    trace_id, span_id = active.newest()
    assert trace_id == format(1, "032x")
    assert span_id == format(20, "016x")


def test_active_spans_falls_back_to_the_parent_when_the_child_ends() -> None:
    active = metrics.ActiveSpans()
    outer, inner = FakeSpan(1, 10), FakeSpan(1, 20)
    active.on_start(outer)
    active.on_start(inner)
    active.on_end(inner)
    assert active.newest() == (format(1, "032x"), format(10, "016x"))


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
    active.on_start(FakeSpan(7, 9))
    sampler = metrics._Sampler(active)
    observations = list(sampler.rss(None))
    assert len(observations) == 1
    attrs = observations[0].attributes
    assert attrs[metrics.TRACE_ID_ATTR] == format(7, "032x")
    assert attrs[metrics.SPAN_ID_ATTR] == format(9, "016x")
    assert observations[0].value > 0


def test_rss_reads_the_live_process() -> None:
    assert metrics._rss_bytes() > 0
