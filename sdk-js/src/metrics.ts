import type { Span, ReadableSpan, SpanProcessor } from '@opentelemetry/sdk-trace-base';

export const TRACE_ID_ATTR = 'capybara.trace_id';
export const SPAN_ID_ATTR = 'capybara.span_id';
export const SPAN_NAME_ATTR = 'capybara.span_name';

export const CPU_METRIC = 'process.cpu.utilization';
export const RSS_METRIC = 'process.memory.usage';

export const DEFAULT_METRICS_ENDPOINT = 'http://127.0.0.1:4318/v1/metrics';
export const INTERVAL_MS = 1000;

export interface ActiveSpanEntry {
  seq: number;
  traceId: string;
  spanId: string;
  name: string;
}

export class ActiveSpansProcessor implements SpanProcessor {
  private _open = new Map<string, ActiveSpanEntry>();
  private _seq = 0;
  private _timer?: NodeJS.Timeout | undefined;

  setTimer(timer: NodeJS.Timeout): void {
    this._timer = timer;
  }

  onStart(span: Span): void {
    const ctx = span.spanContext();
    this._open.set(ctx.spanId, {
      seq: this._seq++,
      traceId: ctx.traceId,
      spanId: ctx.spanId,
      name: span.name,
    });
  }

  onEnd(span: ReadableSpan): void {
    this._open.delete(span.spanContext().spanId);
  }

  shutdown(): Promise<void> {
    if (this._timer) {
      clearInterval(this._timer);
      this._timer = undefined;
    }
    this._open.clear();
    return Promise.resolve();
  }

  forceFlush(): Promise<void> {
    return Promise.resolve();
  }

  newest(): ActiveSpanEntry | undefined {
    let best: ActiveSpanEntry | undefined;
    for (const entry of this._open.values()) {
      if (!best || entry.seq > best.seq) {
        best = entry;
      }
    }
    return best;
  }
}

export function metricsEndpoint(tracesUrl?: string): string {
  if (tracesUrl) {
    if (tracesUrl.endsWith('/v1/traces')) {
      return tracesUrl.slice(0, -'/v1/traces'.length) + '/v1/metrics';
    }
    return tracesUrl.replace(/\/+$/, '') + '/v1/metrics';
  }
  if (process.env.OTEL_EXPORTER_OTLP_METRICS_ENDPOINT) {
    return process.env.OTEL_EXPORTER_OTLP_METRICS_ENDPOINT;
  }
  if (process.env.OTEL_EXPORTER_OTLP_ENDPOINT) {
    return process.env.OTEL_EXPORTER_OTLP_ENDPOINT.replace(/\/+$/, '') + '/v1/metrics';
  }
  return DEFAULT_METRICS_ENDPOINT;
}

export function startMetrics(endpoint: string, active: ActiveSpansProcessor): NodeJS.Timeout {
  let lastUsage = process.cpuUsage();
  let lastTime = process.hrtime.bigint();

  const timer = setInterval(() => {
    const current = active.newest();
    if (!current) {
      return;
    }
    const cpuDiff = process.cpuUsage(lastUsage);
    const nowTime = process.hrtime.bigint();
    const elapsedMicros = Number(nowTime - lastTime) / 1000;
    lastUsage = process.cpuUsage();
    lastTime = nowTime;

    const totalMicros = cpuDiff.user + cpuDiff.system;
    const cpuUtil = elapsedMicros > 0 ? Math.min(1.0, totalMicros / elapsedMicros) : 0;
    const rssBytes = process.memoryUsage().rss;
    const timeNanos = String(Date.now() * 1_000_000);

    const payload = {
      resourceMetrics: [
        {
          scopeMetrics: [
            {
              metrics: [
                {
                  name: CPU_METRIC,
                  gauge: {
                    dataPoints: [
                      {
                        timeUnixNano: timeNanos,
                        asDouble: cpuUtil,
                        attributes: [
                          { key: TRACE_ID_ATTR, value: { stringValue: current.traceId } },
                          { key: SPAN_ID_ATTR, value: { stringValue: current.spanId } },
                          { key: SPAN_NAME_ATTR, value: { stringValue: current.name } },
                        ],
                      },
                    ],
                  },
                },
                {
                  name: RSS_METRIC,
                  gauge: {
                    dataPoints: [
                      {
                        timeUnixNano: timeNanos,
                        asInt: String(rssBytes),
                        attributes: [
                          { key: TRACE_ID_ATTR, value: { stringValue: current.traceId } },
                          { key: SPAN_ID_ATTR, value: { stringValue: current.spanId } },
                          { key: SPAN_NAME_ATTR, value: { stringValue: current.name } },
                        ],
                      },
                    ],
                  },
                },
              ],
            },
          ],
        },
      ],
    };

    fetch(endpoint, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(payload),
    }).catch(() => {});
  }, INTERVAL_MS);

  timer.unref();
  return timer;
}
