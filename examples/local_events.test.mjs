import assert from 'node:assert/strict';
import test from 'node:test';

test('retry reuses the original event body', async () => {
  const bodies = [];
  globalThis.fetch = async (_url, options) => {
    bodies.push(options.body);
    if (bodies.length === 1) {
      return { status: 503, headers: new Headers({ 'Retry-After': '0' }) };
    }
    return { ok: true, status: 201, json: async () => ({ stored: true }) };
  };
  await import('./local_events.mjs');
  assert.equal(bodies.length, 4);
  assert.equal(bodies[0], bodies[1]);
  assert.equal(JSON.parse(bodies[0]).type, 'span.start');
});
