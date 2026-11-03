# capybara-sdk (JavaScript)

Emit OpenTelemetry `gen_ai.*` traces to a local [capybara](https://github.com/tonquoc0407/capybara)
trace debugger, from Node.

```bash
npm install capybara-sdk
```

```ts
import { init, trace, schema } from 'capybara-sdk';

init(); // export to a local capybara on 127.0.0.1:4318

schema('lookup_price', {
  type: 'object',
  properties: { price: { type: 'number' }, currency: { type: 'string' } },
  required: ['price', 'currency'],
}); // declared beats learned

const lookupPrice = trace((sku: string) => ({ price: 42, currency: 'USD' }), {
  tool: 'lookup_price',
});
```

`init()` installs one OTLP exporter to a local capybara and reuses it on a second
call, so an OpenInference or OpenLLMetry instrumentor covering OpenAI, Anthropic,
or the Vercel AI SDK keeps producing its own LLM spans alongside.
Call `init()` before another OpenTelemetry SDK registers a tracer provider. If
your app already owns that provider, configure its OTLP HTTP trace exporter to
send to capybara instead; `init()` reports the conflict rather than claiming to
have installed an exporter that the app will not use.

`trace(fn, opts)` wraps an un-instrumented function and returns the wrapped one.
Without options it records a tool span named after the function; `kind: 'agent'`
or `kind: 'llm'` records other span kinds. A returned promise is awaited so the
tool result lands on the span it belongs to. `schema(tool, jsonSchema)` attaches
a JSON Schema to that tool's spans — the `{type, properties, required, items}`
subset capybara understands, so no schema library is pulled in — and capybara
then reports contract drift on the first violating call.

Node replay re-runs the recorded entrypoint and can substitute recorded tool
outputs. It does not intercept model calls. Use `capybara replay --allow-live <run>`
only when provider calls and their cost are acceptable; the TUI replay action
does not enable this flag.
