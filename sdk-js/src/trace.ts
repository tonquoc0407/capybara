// Span wrapper for un-instrumented agent code.
import { trace as traceApi, SpanStatusCode, type Span } from '@opentelemetry/api';
import { schemaFor, SCHEMA_ATTR } from './schema.js';

const OPERATION: Record<string, string> = { tool: 'execute_tool', agent: 'invoke_agent', llm: 'chat' };

export interface TraceOptions {
  name?: string;
  tool?: string;
  kind?: string;
  // target names the importable tool (module:qualname) so an exported test can
  // call the real function. JavaScript has no portable module path at runtime,
  // so it is opt-in rather than derived.
  target?: string;
}

export let toolServer: ((tool: string, argumentsText: string) => unknown) | undefined;

export function setToolServer(server?: (tool: string, argumentsText: string) => unknown): void {
  toolServer = server;
}

// trace wraps a function so each call is recorded as a capybara span, awaiting a
// returned promise so the tool result lands on the span it belongs to.
export function trace<A extends unknown[], R>(fn: (...a: A) => R, opts: TraceOptions = {}): (...a: A) => R {
  const kind = opts.kind ?? 'tool';
  const toolName = opts.tool ?? (kind === 'tool' ? fn.name || undefined : undefined);
  const spanName = opts.name ?? (toolName ? `execute_tool ${toolName}` : fn.name || 'trace');
  const operation = OPERATION[kind] ?? kind;

  return function (this: unknown, ...args: A): R {
    const tracer = traceApi.getTracer('capybara');
    return tracer.startActiveSpan(spanName, (span): R => {
      try {
        span.setAttribute('gen_ai.operation.name', operation);
        let result: unknown;
        if (toolName !== undefined) {
          span.setAttribute('gen_ai.tool.name', toolName);
          const argStr = dump(args);
          span.setAttribute('gen_ai.tool.call.arguments', argStr);
          if (opts.target !== undefined) {
            span.setAttribute('capybara.target', opts.target);
          }
          const declared = schemaFor(toolName);
          if (declared !== undefined) {
            span.setAttribute(SCHEMA_ATTR, declared);
          }
          result = toolServer !== undefined ? toolServer(toolName, argStr) : fn.apply(this, args);
          if (toolServer !== undefined && fn.constructor?.name === 'AsyncFunction') {
            result = Promise.resolve(result);
          }
        } else {
          result = fn.apply(this, args);
        }
        if (isPromise(result)) {
          return result.then(
            (value) => {
              try {
                recordResult(span, toolName, value);
                return value;
              } catch (err) {
                recordFailure(span, err);
                throw err;
              } finally {
                span.end();
              }
            },
            (err: unknown) => {
              recordFailure(span, err);
              span.end();
              throw err;
            },
          ) as R;
        }
        recordResult(span, toolName, result);
        span.end();
        return result as R;
      } catch (err) {
        recordFailure(span, err);
        span.end();
        throw err;
      }
    });
  };
}

function recordFailure(span: Span, err: unknown): void {
  span.recordException(err instanceof Error ? err : String(err));
  span.setStatus({ code: SpanStatusCode.ERROR, message: err instanceof Error ? err.message : String(err) });
}

function recordResult(span: Span, toolName: string | undefined, result: unknown): void {
  if (toolName !== undefined) {
    span.setAttribute('gen_ai.tool.call.result', dump(result));
  }
}

function dump(value: unknown): string {
  return JSON.stringify(value) ?? String(value);
}

function isPromise(value: unknown): value is Promise<unknown> {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as { then?: unknown }).then === 'function'
  );
}
