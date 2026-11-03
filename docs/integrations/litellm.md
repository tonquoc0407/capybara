# LiteLLM Proxy → capybara

Capybara observes LiteLLM through OTLP; LiteLLM remains the gateway and handles provider routing. This configuration follows [LiteLLM's OpenTelemetry v2 guide](https://docs.litellm.ai/docs/observability/langfuse_otel_integration). The integration has been checked against the documented settings, but this repository does not run a LiteLLM process in CI.

Start local capybara first. Then start LiteLLM Proxy with:

```sh
LITELLM_OTEL_V2=true \
OTEL_EXPORTER=otlp_http \
OTEL_ENDPOINT=http://127.0.0.1:4318 \
litellm --config config.yaml
```

Make one request through LiteLLM, then run `capybara doctor` and `capybara runs`. For a shared hub, point `OTEL_ENDPOINT` at the hub's TLS reverse proxy and set the project editor token using LiteLLM's `OTEL_HEADERS` setting. The hub itself binds localhost and does not terminate TLS. LiteLLM's content-capture setting and capybara's `-no-content`/hub capture policy both affect whether a prompt body is available; verify this before relying on replay or dataset creation.

LiteLLM owns pricing and routing decisions. Capybara's trace cost is an estimate unless a provider-reported amount and its source are recorded. The OTLP trace does not make arbitrary agent code replayable.
