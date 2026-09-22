import { test, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import {
  Session,
  ReplayError,
  hashToolCall,
  FixedTraceIdGenerator,
  type Manifest,
} from '../dist/replay.js';
import { trace, setToolServer } from '../dist/index.js';

afterEach(() => {
  setToolServer(undefined);
});

test('hashToolCall produces consistent SHA256 hex string', () => {
  const h1 = hashToolCall('get_price', '["NVDA"]');
  const h2 = hashToolCall('get_price', '["NVDA"]');
  const h3 = hashToolCall('get_price', '["AAPL"]');
  assert.equal(h1, h2);
  assert.notEqual(h1, h3);
  assert.equal(h1.length, 64);
});

test('Session serves recorded tool outputs and sets diverged flag on edited tool', () => {
  const hash1 = hashToolCall('get_price', '["NVDA"]');
  const hash2 = hashToolCall('lookup', '["X"]');

  const manifest: Manifest = {
    version: 1,
    run_id: '0123456789abcdef0123456789abcdef',
    parent_run_id: 'parent123',
    endpoint: 'http://127.0.0.1:4318',
    entrypoint: ['node', 'index.js'],
    cwd: process.cwd(),
    tools: [
      { hash: hash1, span_id: 's1', tool: 'get_price', output: '{"price":100}' },
      { hash: hash2, span_id: 's2', tool: 'lookup', output: '{"found":true}', edited: true },
    ],
  };

  const session = new Session(manifest);
  assert.equal(session.diverged, false);

  const res1 = session.serveTool('get_price', '["NVDA"]');
  assert.deepEqual(res1, { price: 100 });
  assert.equal(session.diverged, false);

  const res2 = session.serveTool('lookup', '["X"]');
  assert.deepEqual(res2, { found: true });
  assert.equal(session.diverged, true);
});

test('Session throws ReplayError when called with unrecorded tool or arguments', () => {
  const manifest: Manifest = {
    version: 1,
    run_id: '0123456789abcdef0123456789abcdef',
    parent_run_id: 'parent123',
    endpoint: 'http://127.0.0.1:4318',
    entrypoint: ['node', 'index.js'],
    cwd: process.cwd(),
    tools: [],
  };

  const session = new Session(manifest);
  assert.throws(
    () => session.serveTool('missing_tool', '[]'),
    (err: unknown) => err instanceof ReplayError && /missing_tool was called with arguments/.test(err.message),
  );
});

test('trace function invokes toolServer when installed', () => {
  const mockServer = (tool: string, args: string) => {
    assert.equal(tool, 'calc');
    assert.equal(args, '[21]');
    return 42;
  };
  setToolServer(mockServer);

  let realCalled = false;
  const myTool = trace((n: number) => {
    realCalled = true;
    return n * 2;
  }, { tool: 'calc' });

  const result = myTool(21);
  assert.equal(result, 42);
  assert.equal(realCalled, false);
});

test('FixedTraceIdGenerator returns the exact trace_id from manifest', () => {
  const gen = new FixedTraceIdGenerator('1234567890abcdef1234567890abcdef');
  assert.equal(gen.generateTraceId(), '1234567890abcdef1234567890abcdef');
  const spanId = gen.generateSpanId();
  assert.equal(spanId.length, 16);
});
