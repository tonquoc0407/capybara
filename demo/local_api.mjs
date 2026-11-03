// Synthetic local API demo. No provider key or network service is required.
import { randomUUID } from 'node:crypto';

const endpoint = process.env.CAPYBARA_EVENTS_URL ?? 'http://127.0.0.1:4318/api/v1/events';
const runId = `demo-local-${randomUUID()}`;
const rootId = randomUUID();
const toolId = randomUUID();
const llmId = randomUUID();
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));

async function send(spanId, fields) {
  const event = {
    event_id: randomUUID(), type: 'span.complete', run_id: runId, span_id: spanId,
    time: new Date().toISOString(), start_time: new Date(Date.now() - 800).toISOString(),
    status: 'ok', ...fields,
  };
  const response = await fetch(endpoint, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(event), signal: AbortSignal.timeout(5000),
  });
  if (!response.ok) throw new Error(`event rejected: ${response.status} ${await response.text()}`);
}

await send(rootId, { kind: 'agent', name: 'demo: price answer after tool failure' });
await pause(800);
await send(toolId, { kind: 'tool', name: 'lookup_price', tool_name: 'lookup_price',
  parent_id: rootId, contents: [{ role: 'output', body: '{"error":"upstream timeout"}', media_type: 'application/json' }] });
await pause(800);
await send(llmId, { kind: 'llm', name: 'answer', parent_id: rootId,
  model: 'gpt-4o-mini', tokens_in: 1200, tokens_out: 80,
  contents: [{ role: 'assistant', body: 'lookup_price says the price is $42.', media_type: 'text/plain' }] });
console.log(runId);
