// Tool output schema registry: JSON Schemas attached to matching tool spans.
import type { Span, SpanProcessor, ReadableSpan } from '@opentelemetry/sdk-trace-base';

export const SCHEMA_ATTR = 'capybara.schema';
const TOOL_NAME_ATTRS = ['gen_ai.tool.name', 'mcp.tool.name'];

const registry = new Map<string, string>();

// schema registers a tool's declared output shape. capybara understands the
// JSON Schema subset {type, properties, required, items}, so a plain object
// works and no schema library is pulled in.
export function schema<T>(toolName: string, jsonSchema: T): T {
  registry.set(toolName, typeof jsonSchema === 'string' ? jsonSchema : JSON.stringify(jsonSchema));
  return jsonSchema;
}

export function schemaFor(toolName: string): string | undefined {
  return registry.get(toolName);
}

// SchemaSpanProcessor attaches a declared schema to a third-party tool span
// whose tool name is already set at span start; trace() attaches its own.
export class SchemaSpanProcessor implements SpanProcessor {
  onStart(span: Span): void {
    const attrs = (span as unknown as { attributes: Record<string, unknown> }).attributes ?? {};
    for (const key of TOOL_NAME_ATTRS) {
      const name = attrs[key];
      if (typeof name === 'string') {
        const declared = registry.get(name);
        if (declared !== undefined) {
          span.setAttribute(SCHEMA_ATTR, declared);
          return;
        }
      }
    }
  }
  onEnd(_span: ReadableSpan): void {}
  shutdown(): Promise<void> {
    return Promise.resolve();
  }
  forceFlush(): Promise<void> {
    return Promise.resolve();
  }
}
