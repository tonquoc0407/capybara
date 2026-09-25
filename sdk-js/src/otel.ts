// OTel bootstrap: a provider plus an OTLP exporter to a local capybara.
import { trace as traceApi, isSpanContextValid, type Context } from '@opentelemetry/api';
import { NodeTracerProvider, BatchSpanProcessor } from '@opentelemetry/sdk-trace-node';
import type { Span, SpanProcessor, ReadableSpan } from '@opentelemetry/sdk-trace-base';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { resourceFromAttributes } from '@opentelemetry/resources';
import { SchemaSpanProcessor } from './schema.js';
import { ActiveSpansProcessor, metricsEndpoint, startMetrics } from './metrics.js';

export const DEFAULT_ENDPOINT = 'http://127.0.0.1:4318/v1/traces';
export const ENTRYPOINT_ATTR = 'capybara.entrypoint';
export const CWD_ATTR = 'capybara.cwd';

let provider: NodeTracerProvider | undefined;

// Time-travel re-executes the recorded process, so the root span carries how it
// was launched. Resource attributes would not survive capybara's span mapping.
export class EntrypointSpanProcessor implements SpanProcessor {
  onStart(span: Span, parentContext: Context): void {
    const parent = traceApi.getSpanContext(parentContext);
    if (parent && isSpanContextValid(parent)) {
      return;
    }
    span.setAttribute(ENTRYPOINT_ATTR, JSON.stringify([process.execPath, ...process.argv.slice(1)]));
    span.setAttribute(CWD_ATTR, process.cwd());
  }
  onEnd(_span: ReadableSpan): void {}
  shutdown(): Promise<void> {
    return Promise.resolve();
  }
  forceFlush(): Promise<void> {
    return Promise.resolve();
  }
}

// resolveEndpoint returns undefined when the OTel exporter should resolve
// OTEL_EXPORTER_OTLP_* itself, so moving the collector with `capybara -otlp`
// needs no code change here.
export function resolveEndpoint(endpoint?: string): string | undefined {
  if (endpoint !== undefined) {
    return endpoint;
  }
  for (const v of ['OTEL_EXPORTER_OTLP_TRACES_ENDPOINT', 'OTEL_EXPORTER_OTLP_ENDPOINT']) {
    if (process.env[v]) {
      return undefined;
    }
  }
  return DEFAULT_ENDPOINT;
}

export interface InitOptions {
  serviceName?: string;
  endpoint?: string;
  metrics?: boolean;
}

// init exports spans to a local capybara. Calling it twice keeps the first
// provider, mirroring an app that only wants one tracer pipeline.
//
// metrics defaults to on for a local capybara and off for any other
// collector: the readings carry a span id, and that is a cardinality a real
// metrics backend should not be handed without being asked.
export function init(opts: InitOptions = {}): NodeTracerProvider {
  if (provider !== undefined) {
    return provider;
  }
  const url = resolveEndpoint(opts.endpoint);
  const enableMetrics = opts.metrics ?? (url === DEFAULT_ENDPOINT);

  const spanProcessors: SpanProcessor[] = [
    new EntrypointSpanProcessor(),
    new SchemaSpanProcessor(),
  ];

  if (enableMetrics) {
    const activeSpans = new ActiveSpansProcessor();
    spanProcessors.push(activeSpans);
    const timer = startMetrics(metricsEndpoint(url), activeSpans);
    activeSpans.setTimer(timer);
  }

  spanProcessors.push(new BatchSpanProcessor(new OTLPTraceExporter(url !== undefined ? { url } : {})));

  provider = new NodeTracerProvider({
    resource: resourceFromAttributes({ 'service.name': opts.serviceName ?? 'capybara' }),
    spanProcessors,
  });
  provider.register();
  return provider;
}

export function _resetForTests(): void {
  provider = undefined;
}
