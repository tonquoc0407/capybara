// Replay runner: re-executes a recorded run against its own recording.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { createHash, randomBytes } from 'node:crypto';
import { NodeTracerProvider, SimpleSpanProcessor } from '@opentelemetry/sdk-trace-node';
import type { IdGenerator } from '@opentelemetry/sdk-trace-base';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { resourceFromAttributes } from '@opentelemetry/resources';
import { setToolServer } from './trace.js';
import { EntrypointSpanProcessor } from './otel.js';
import { SchemaSpanProcessor } from './schema.js';

export class ReplayError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ReplayError';
  }
}

export function hashToolCall(tool: string, argumentsText: string): string {
  return createHash('sha256').update(`${tool}\x00${argumentsText}`).digest('hex');
}

export interface ToolEntry {
  hash: string;
  span_id: string;
  tool: string;
  output: string;
  edited?: boolean;
}

export interface LLMEntry {
  hash: string;
  span_id: string;
  model: string;
  response: string;
}

export interface Manifest {
  version: number;
  run_id: string;
  parent_run_id: string;
  start_span_id?: string;
  endpoint: string;
  entrypoint: string[];
  cwd: string;
  llm?: LLMEntry[];
  tools?: ToolEntry[];
}

export class Session {
  public readonly manifest: Manifest;
  public readonly tools: Map<string, ToolEntry>;
  public readonly llm: Map<string, LLMEntry>;
  public readonly edited: string | undefined;
  public diverged: boolean = false;

  constructor(manifest: Manifest) {
    this.manifest = manifest;
    this.tools = new Map((manifest.tools ?? []).map((e) => [e.hash, e]));
    this.llm = new Map((manifest.llm ?? []).map((e) => [e.hash, e]));
    const editedEntry = (manifest.tools ?? []).find((e) => e.edited);
    this.edited = editedEntry?.hash;
  }

  serveTool(tool: string, argumentsText: string): unknown {
    const key = hashToolCall(tool, argumentsText);
    const entry = this.tools.get(key);
    if (!entry) {
      throw new ReplayError(
        `tool ${tool} was called with arguments that are not in the recording`
      );
    }
    if (entry.hash === this.edited) {
      this.diverged = true;
    }
    return decodeOutput(entry.output);
  }

  install(): void {
    setToolServer(this.serveTool.bind(this));
  }
}

function decodeOutput(output: string): unknown {
  try {
    return JSON.parse(output);
  } catch {
    return output;
  }
}

export class FixedTraceIdGenerator implements IdGenerator {
  constructor(private readonly traceId: string) {}

  generateTraceId(): string {
    return this.traceId;
  }

  generateSpanId(): string {
    return randomBytes(8).toString('hex');
  }
}

export function providerFor(manifest: Manifest): NodeTracerProvider {
  const exporter = new OTLPTraceExporter({ url: manifest.endpoint });
  const provider = new NodeTracerProvider({
    resource: resourceFromAttributes({ 'service.name': 'capybara-replay' }),
    idGenerator: new FixedTraceIdGenerator(manifest.run_id),
    spanProcessors: [
      new EntrypointSpanProcessor(),
      new SchemaSpanProcessor(),
      new SimpleSpanProcessor(exporter),
    ],
  });
  provider.register();
  return provider;
}

export async function run(manifestPath: string): Promise<void> {
  const raw = readFileSync(manifestPath, 'utf8');
  const manifest = JSON.parse(raw) as Manifest;
  if (manifest.version !== 1) {
    throw new ReplayError(`unsupported manifest version ${manifest.version}`);
  }
  const session = new Session(manifest);
  session.install();
  const provider = providerFor(manifest);

  const entrypoint = manifest.entrypoint;
  if (!entrypoint || entrypoint.length < 2) {
    throw new ReplayError('recorded entrypoint has no script to run');
  }
  const script = entrypoint[1];
  if (!script) {
    throw new ReplayError('recorded entrypoint has empty script');
  }
  process.argv = [entrypoint[0] ?? process.execPath, ...entrypoint.slice(1)];
  const scriptPath = resolve(manifest.cwd, script);
  try {
    await import(pathToFileURL(scriptPath).href);
  } finally {
    await provider.forceFlush();
    await provider.shutdown();
  }
}
