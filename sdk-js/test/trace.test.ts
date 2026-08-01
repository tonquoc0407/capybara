import { test, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { trace as traceApi } from '@opentelemetry/api';
import {
  NodeTracerProvider,
  SimpleSpanProcessor,
  InMemorySpanExporter,
} from '@opentelemetry/sdk-trace-node';
import { trace, schema } from '../dist/index.js';
import { EntrypointSpanProcessor, ENTRYPOINT_ATTR, CWD_ATTR } from '../dist/otel.js';
import { SchemaSpanProcessor, SCHEMA_ATTR } from '../dist/schema.js';

const exporter = new InMemorySpanExporter();
const provider = new NodeTracerProvider({
  spanProcessors: [
    new EntrypointSpanProcessor(),
    new SchemaSpanProcessor(),
    new SimpleSpanProcessor(exporter),
  ],
});
provider.register();

beforeEach(() => exporter.reset());

function only() {
  const spans = exporter.getFinishedSpans();
  assert.equal(spans.length, 1);
  return spans[0]!;
}

test('a tool call records the gen_ai attributes capybara ingests', () => {
  const getPrice = trace((ticker: string) => ({ price: 42, ticker }), { tool: 'get_price' });
  const out = getPrice('NVDA');
  assert.deepEqual(out, { price: 42, ticker: 'NVDA' });
  const s = only();
  assert.equal(s.name, 'execute_tool get_price');
  assert.equal(s.attributes['gen_ai.operation.name'], 'execute_tool');
  assert.equal(s.attributes['gen_ai.tool.name'], 'get_price');
  assert.equal(s.attributes['gen_ai.tool.call.arguments'], JSON.stringify(['NVDA']));
  assert.equal(s.attributes['gen_ai.tool.call.result'], JSON.stringify({ price: 42, ticker: 'NVDA' }));
});

test('the root span carries the entrypoint for replay', () => {
  trace(() => 1, { tool: 'root_tool' })();
  const s = only();
  assert.ok(typeof s.attributes[ENTRYPOINT_ATTR] === 'string');
  assert.equal(s.attributes[CWD_ATTR], process.cwd());
});

test('an async tool awaits the result onto its span', async () => {
  const fetchIt = trace(async (id: number) => ({ ok: true, id }), { tool: 'fetch_it' });
  const out = await fetchIt(7);
  assert.deepEqual(out, { ok: true, id: 7 });
  assert.equal(only().attributes['gen_ai.tool.call.result'], JSON.stringify({ ok: true, id: 7 }));
});

test('agent kind maps to invoke_agent and carries no tool attributes', () => {
  trace(() => 'done', { name: 'plan', kind: 'agent' })();
  const s = only();
  assert.equal(s.attributes['gen_ai.operation.name'], 'invoke_agent');
  assert.equal(s.attributes['gen_ai.tool.name'], undefined);
});

test('a declared schema rides on the tool span', () => {
  const shape = { type: 'object', properties: { price: { type: 'number' } }, required: ['price'] };
  schema('lookup', shape);
  trace(() => ({ price: 1 }), { tool: 'lookup' })();
  assert.equal(only().attributes[SCHEMA_ATTR], JSON.stringify(shape));
});

test('a declared schema attaches to a third-party tool span at start', () => {
  schema('external_tool', { type: 'array' });
  traceApi
    .getTracer('test')
    .startSpan('external', { attributes: { 'gen_ai.tool.name': 'external_tool' } })
    .end();
  assert.equal(only().attributes[SCHEMA_ATTR], JSON.stringify({ type: 'array' }));
});

test('a throwing tool still ends its span and rethrows', () => {
  const boom = trace(
    () => {
      throw new Error('boom');
    },
    { tool: 'boom' },
  );
  assert.throws(() => boom(), /boom/);
  assert.equal(exporter.getFinishedSpans().length, 1);
});
