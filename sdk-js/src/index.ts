// capybara-sdk: emit OpenTelemetry traces to a local capybara trace debugger.
export { init, resolveEndpoint, DEFAULT_ENDPOINT } from './otel.js';
export type { InitOptions } from './otel.js';
export { trace } from './trace.js';
export type { TraceOptions } from './trace.js';
export { schema } from './schema.js';
export { ActiveSpansProcessor, startMetrics, metricsEndpoint } from './metrics.js';
export type { ActiveSpanEntry } from './metrics.js';

export const version = '0.1.0';
