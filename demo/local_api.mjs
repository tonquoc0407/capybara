// Synthetic local API demo. No provider key or network service is required.
import { randomUUID } from 'node:crypto';

const endpoint = process.env.CAPYBARA_EVENTS_URL ?? 'http://127.0.0.1:4318/api/v1/events';
const runId = `demo-local-${randomUUID()}`;
const rootId = randomUUID();
const planId = randomUUID();
const contextId = randomUUID();
const priceId = randomUUID();
const cacheId = randomUUID();
const answerId = randomUUID();
const started = Date.now() - 3000;
const stamp = ms => new Date(started + ms).toISOString();
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));

async function send(spanId, startMs, endMs, fields) {
  const event = {
    event_id: randomUUID(), type: 'span.complete', run_id: runId, span_id: spanId,
    time: stamp(endMs), start_time: stamp(startMs),
    status: 'ok', ...fields,
  };
  const response = await fetch(endpoint, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(event), signal: AbortSignal.timeout(5000),
  });
  if (!response.ok) throw new Error(`event rejected: ${response.status} ${await response.text()}`);
}

await send(planId, 100, 520, { kind: 'llm', name: 'plan quote', parent_id: rootId,
  model: 'gpt-4o-mini', tokens_in: 300, tokens_out: 40,
  contents: [{ role: 'assistant', body: 'Call lookup_price before quoting a price.', media_type: 'text/plain' }] });
await pause(120);
await send(contextId, 560, 790, { kind: 'retrieval', name: 'portfolio context', parent_id: rootId,
  contents: [{ role: 'output', body: 'Portfolio holds 120 ACME shares.', media_type: 'text/plain' }] });
await pause(120);
await send(priceId, 830, 1390, { kind: 'tool', name: 'lookup_price', tool_name: 'lookup_price',
  parent_id: rootId, status: 'error',
  contents: [{ role: 'output', body: '{"error":"upstream timeout"}', media_type: 'application/json' }] });
await pause(120);
await send(cacheId, 1450, 1690, { kind: 'tool', name: 'read_cached_quote', tool_name: 'read_cached_quote',
  parent_id: rootId, contents: [{ role: 'output', body: '{"price":null}', media_type: 'application/json' }] });
await pause(120);
await send(answerId, 1760, 2460, { kind: 'llm', name: 'answer', parent_id: rootId,
  model: 'gpt-4o-mini', tokens_in: 1200, tokens_out: 80,
  contents: [{ role: 'assistant', body: 'lookup_price says the price is $42.', media_type: 'text/plain' }] });
await pause(120);
await send(rootId, 0, 2600, { kind: 'agent', name: 'demo: price answer after tool failure' });
console.log(runId);
