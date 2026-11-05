# capybara-sdk

Emit OpenTelemetry `gen_ai.*` traces to a local [capybara](https://github.com/tonquoc0407/capybara)
trace debugger. Import name is `capybara`.

```bash
pip install capybara-sdk
```

```python
import capybara
from pydantic import BaseModel

capybara.init()  # export to a local capybara on 127.0.0.1:4318


class Price(BaseModel):
    price: float
    currency: str


capybara.schema("lookup_price", Price)  # check this declared shape


@capybara.trace(tool="lookup_price")
def lookup_price(sku: str) -> dict:
    return {"price": 42.0, "currency": "USD"}
```

`init()` reuses an existing OpenTelemetry SDK `TracerProvider` when one is
installed. It adds an OTLP exporter and processors for schema and replay
metadata. Resource sampling starts by default for the local endpoint.

`@capybara.trace` wraps un-instrumented functions. A bare `@capybara.trace`
records a tool span named after the function; `kind="agent"` or `kind="llm"`
records other span kinds. `capybara.schema(tool, model)` attaches a Pydantic
model's JSON Schema to that tool's spans so capybara reports contract drift on
the first violating call.

Sync and async functions are both supported.

`capybara.replay` is invoked by the binary: pressing `r` in the TUI re-executes
the recorded process and serves model responses and tool outputs that were
recorded. An unrecorded model call stops by default. To permit a provider call
after an edited tool output, use `capybara replay --allow-live <run>`; that call
may cost money. The replayed application can also perform its own network I/O.
