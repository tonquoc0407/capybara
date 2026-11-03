// Run with Node 18+ while capybara is listening locally.
import { randomUUID } from 'node:crypto';

const endpoint = process.env.CAPYBARA_EVENTS_URL ?? 'http://127.0.0.1:4318/api/v1/events';
const runId = randomUUID();
const spanId = randomUUID();

async function send(type, fields = {}) {
  const body = JSON.stringify({
    event_id: randomUUID(), type, run_id: runId, span_id: spanId,
    time: new Date().toISOString(), ...fields,
  });
  for (let attempt = 0; attempt < 3; attempt++) {
    let response;
    try {
      response = await fetch(endpoint, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body, signal: AbortSignal.timeout(5000),
      });
    } catch (error) {
      if (attempt === 2) throw error;
      await new Promise(resolve => setTimeout(resolve, 200 * 2 ** attempt));
      continue;
    }
    if (response.status === 503 && attempt < 2) {
      const seconds = Number(response.headers.get('Retry-After') ?? '1');
      await response.body?.cancel();
      await new Promise(resolve => setTimeout(resolve, Math.min(Math.max(seconds, 0), 2) * 1000));
      continue;
    }
    if (!response.ok) throw new Error(`capybara ${response.status}: ${await response.text()}`);
    return response.json();
  }
}

await send('span.start', { kind: 'llm', name: 'example model call', model: 'gpt-4o-mini' });
await send('span.update', { tokens_in: 12 });
await send('span.end', { status: 'ok', tokens_out: 8 });
console.log(runId);
