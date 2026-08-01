// Span wrapper for un-instrumented agent code.
import { trace as traceApi, type Span } from '@opentelemetry/api';
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
      span.setAttribute('gen_ai.operation.name', operation);
      if (toolName !== undefined) {
        span.setAttribute('gen_ai.tool.name', toolName);
        span.setAttribute('gen_ai.tool.call.arguments', dump(args));
        if (opts.target !== undefined) {
          span.setAttribute('capybara.target', opts.target);
        }
        const declared = schemaFor(toolName);
        if (declared !== undefined) {
          span.setAttribute(SCHEMA_ATTR, declared);
        }
      }
      let result: R;
      try {
        result = fn.apply(this, args);
      } catch (err) {
        span.recordException(err as Error);
        span.end();
        throw err;
      }
      if (isPromise(result)) {
        return result.then(
          (value) => {
            recordResult(span, toolName, value);
            span.end();
            return value;
          },
          (err: unknown) => {
            span.recordException(err as Error);
            span.end();
            throw err;
          },
        ) as R;
      }
      recordResult(span, toolName, result);
      span.end();
      return result;
    });
  };
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
