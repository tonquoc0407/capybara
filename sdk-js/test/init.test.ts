import { test } from 'node:test';
import assert from 'node:assert/strict';
import { init } from '../dist/index.js';
import { resolveEndpoint, DEFAULT_ENDPOINT } from '../dist/otel.js';

test('resolveEndpoint prefers an explicit endpoint', () => {
  assert.equal(resolveEndpoint('http://host:1234/v1/traces'), 'http://host:1234/v1/traces');
});

test('resolveEndpoint falls back to a local capybara', () => {
  delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
  delete process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT;
  assert.equal(resolveEndpoint(), DEFAULT_ENDPOINT);
});

test('resolveEndpoint yields to an OTEL environment variable', () => {
  process.env.OTEL_EXPORTER_OTLP_ENDPOINT = 'http://collector:4318';
  try {
    assert.equal(resolveEndpoint(), undefined);
  } finally {
    delete process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
  }
});

test('init installs one provider and reuses it', async () => {
  const provider = init();
  assert.equal(init(), provider);
  await provider.shutdown();
});
