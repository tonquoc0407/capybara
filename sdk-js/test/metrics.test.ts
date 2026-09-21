import { test, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import {
  ActiveSpansProcessor,
  metricsEndpoint,
  DEFAULT_ENDPOINT,
  init,
} from '../dist/index.js';
import { _resetForTests } from '../dist/otel.js';
import type { Span, ReadableSpan } from '@opentelemetry/sdk-trace-base';

function fakeSpan(traceId: string, spanId: string, name: string): Span {
  return {
    name,
    spanContext() {
      return {
        traceId,
        spanId,
        traceFlags: 1,
      };
    },
  } as unknown as Span;
}

function fakeReadableSpan(spanId: string): ReadableSpan {
  return {
    spanContext() {
      return {
        traceId: '1',
        spanId,
        traceFlags: 1,
      };
    },
  } as unknown as ReadableSpan;
}

test('metricsEndpoint transforms trace endpoints to metrics endpoints', () => {
  assert.equal(
    metricsEndpoint('http://127.0.0.1:4318/v1/traces'),
    'http://127.0.0.1:4318/v1/metrics'
  );
  assert.equal(
    metricsEndpoint('http://collector:4318'),
    'http://collector:4318/v1/metrics'
  );
  assert.equal(
    metricsEndpoint('http://collector:4318/'),
    'http://collector:4318/v1/metrics'
  );
});

test('metricsEndpoint falls back to env vars or default', () => {
  delete process.env.OTEL_EXPORTER_OTLP_METRICS_ENDPOINT;
  delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
  assert.equal(metricsEndpoint(), 'http://127.0.0.1:4318/v1/metrics');

  process.env.OTEL_EXPORTER_OTLP_ENDPOINT = 'http://collector:4318';
  assert.equal(metricsEndpoint(), 'http://collector:4318/v1/metrics');

  process.env.OTEL_EXPORTER_OTLP_METRICS_ENDPOINT = 'http://collector:4318/v1/custom-metrics';
  assert.equal(metricsEndpoint(), 'http://collector:4318/v1/custom-metrics');

  delete process.env.OTEL_EXPORTER_OTLP_METRICS_ENDPOINT;
  delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
});

test('ActiveSpansProcessor tracks newest open span and falls back on end', () => {
  const active = new ActiveSpansProcessor();
  assert.equal(active.newest(), undefined);

  const parent = fakeSpan('00000000000000000000000000000001', '000000000000000a', 'parent');
  const child = fakeSpan('00000000000000000000000000000001', '0000000000000014', 'child');

  active.onStart(parent);
  assert.equal(active.newest()?.name, 'parent');
  assert.equal(active.newest()?.spanId, '000000000000000a');

  active.onStart(child);
  assert.equal(active.newest()?.name, 'child');
  assert.equal(active.newest()?.spanId, '0000000000000014');

  active.onEnd(fakeReadableSpan('0000000000000014'));
  assert.equal(active.newest()?.name, 'parent');
  assert.equal(active.newest()?.spanId, '000000000000000a');

  active.onEnd(fakeReadableSpan('000000000000000a'));
  assert.equal(active.newest(), undefined);
});

test('ActiveSpansProcessor shutdown clears open spans and timer', async () => {
  const active = new ActiveSpansProcessor();
  let timerCleared = false;
  const dummyTimer = setInterval(() => {}, 10000);
  active.setTimer(dummyTimer);

  const span = fakeSpan('00000000000000000000000000000001', '000000000000000a', 'test');
  active.onStart(span);
  assert.notEqual(active.newest(), undefined);

  await active.shutdown();
  assert.equal(active.newest(), undefined);
});

test('init configures metrics according to endpoint and options', async () => {
  // Test local default: metrics should be enabled
  _resetForTests();
  delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
  delete process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT;
  const p1 = init();
  const p1Processors = (p1 as any)._activeSpanProcessor?._spanProcessors ?? [];
  const hasActiveSpans1 = p1Processors.some((sp: any) => sp instanceof ActiveSpansProcessor);
  assert.equal(hasActiveSpans1, true);
  await p1.shutdown();

  // Test local with metrics: false
  _resetForTests();
  const p2 = init({ metrics: false });
  const p2Processors = (p2 as any)._activeSpanProcessor?._spanProcessors ?? [];
  const hasActiveSpans2 = p2Processors.some((sp: any) => sp instanceof ActiveSpansProcessor);
  assert.equal(hasActiveSpans2, false);
  await p2.shutdown();

  // Test foreign collector: metrics defaults to false
  _resetForTests();
  process.env.OTEL_EXPORTER_OTLP_ENDPOINT = 'http://collector.internal:4318';
  try {
    const p3 = init();
    const p3Processors = (p3 as any)._activeSpanProcessor?._spanProcessors ?? [];
    const hasActiveSpans3 = p3Processors.some((sp: any) => sp instanceof ActiveSpansProcessor);
    assert.equal(hasActiveSpans3, false);
    await p3.shutdown();

    // Test foreign collector with metrics: true forced
    _resetForTests();
    const p4 = init({ metrics: true });
    const p4Processors = (p4 as any)._activeSpanProcessor?._spanProcessors ?? [];
    const hasActiveSpans4 = p4Processors.some((sp: any) => sp instanceof ActiveSpansProcessor);
    assert.equal(hasActiveSpans4, true);
    await p4.shutdown();
  } finally {
    delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
    _resetForTests();
  }
});
