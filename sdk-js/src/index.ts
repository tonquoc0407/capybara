// capybara-sdk: emit OpenTelemetry traces to a local capybara trace debugger.
export { init } from './otel.js';
export type { InitOptions } from './otel.js';
export { trace } from './trace.js';
export type { TraceOptions } from './trace.js';
export { schema } from './schema.js';

export const version = '0.1.0';
