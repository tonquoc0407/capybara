import { SpanStatusCode } from '@opentelemetry/api';
import { NodeTracerProvider, SimpleSpanProcessor, InMemorySpanExporter } from '@opentelemetry/sdk-trace-node';
import { trace } from '../dist/index.js';

const exporter = new InMemorySpanExporter();
const provider = new NodeTracerProvider({ spanProcessors: [new SimpleSpanProcessor(exporter)] });
provider.register();

const lookupPrice = trace(
  async () => { throw new Error('price service unavailable'); },
  { tool: 'lookupPrice' },
);
try {
  await lookupPrice();
} catch (error) {
  console.log(`app error: ${error.message}`);
}

const span = exporter.getFinishedSpans()[0];
console.log(`span: ${span.name}`);
console.log(`status: ${span.status.code === SpanStatusCode.ERROR ? 'ERROR' : 'unexpected'}`);
console.log(`exception: ${span.events[0]?.attributes?.['exception.message']}`);
await provider.shutdown();
