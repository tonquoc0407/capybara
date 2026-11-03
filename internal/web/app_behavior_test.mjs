// Lightweight DOM contract tests for the embedded workspace, with no browser
// dependency or frontend build step. Run: node --test internal/web/app_behavior_test.mjs
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import vm from "node:vm";

const script = readFileSync(new URL("./assets/app.js", import.meta.url), "utf8");

class Element {
  constructor(tag, id = "") {
    this.tagName = tag.toUpperCase(); this.id = id; this.children = []; this.dataset = {};
    this.listeners = {}; this.value = ""; this._text = ""; this.className = "";
    this.classList = { toggle: () => {} };
  }
  set textContent(value) { this._text = String(value); this.children = []; }
  get textContent() { return this._text + this.children.map((c) => c.textContent || "").join(""); }
  append(...items) { this.children.push(...items); }
  replaceChildren(...items) { this.children = items; this._text = ""; }
  addEventListener(name, callback) { this.listeners[name] = callback; }
  trigger(name) { this.listeners[name]?.({ preventDefault() {}, stopPropagation() {} }); }
  setAttribute(name, value) { this[name] = value; }
  querySelector(selector) { return find(this, (e) => e.tagName.toLowerCase() === selector.toLowerCase()); }
}

function find(root, predicate) {
  if (predicate(root)) return root;
  for (const child of root.children) {
    const match = find(child, predicate);
    if (match) return match;
  }
  return null;
}

function harness(href, inline, routes, stream = false) {
  const root = new Element("main", "workspace");
  const status = new Element("footer", "status");
  const project = new Element("input", "project");
  const projects = new Element("datalist", "projects");
  const refresh = new Element("button", "refresh");
  const nav = ["overview", "explore", "trace", "datasets", "prompts", "usage", "settings"].map((view) => {
    const a = new Element("a"); a.dataset.view = view; return a;
  });
  const elements = { workspace: root, status, project, projects, refresh };
  const location = { href };
  const requested = [];
  const requests = [];
  const listeners = {}, sources = [];
  const document = {
    hidden: false, activeElement: new Element("body"),
    createElement: (tag) => new Element(tag), getElementById: (id) => elements[id],
    querySelectorAll: (selector) => selector === "nav a" ? nav : [],
    addEventListener(name, callback) { listeners[name] = callback; },
  };
  const history = {
    pushState(_a, _b, next) { location.href = new URL(next, location.href).href; },
    replaceState(_a, _b, next) { location.href = new URL(next, location.href).href; },
  };
  const fetch = async (path, options) => {
    requested.push(path);
    requests.push({ path, options });
    const key = path.split("?")[0];
    if (!(key in routes)) return { ok: false, status: 404, text: async () => "missing" };
    return { ok: true, status: 200, json: async () => routes[key], text: async () => JSON.stringify(routes[key]) };
  };
  const window = { CAPYBARA: inline, addEventListener() {} };
  const EventSource = stream ? class {
    constructor() { this.listeners = {}; sources.push(this); }
    addEventListener(name, callback) { this.listeners[name] = callback; }
  } : undefined;
  vm.runInNewContext(script, { window, document, location, history, fetch, URL, URLSearchParams,
    setTimeout, clearTimeout, EventSource, console });
  return { root, status, location, requested, requests, nav, refresh,
    input() { listeners.input?.(); }, emitUpdate() { sources.forEach((s) => s.listeners.update?.()); } };
}

async function settle() { for (let i = 0; i < 5; i++) await new Promise((resolve) => setImmediate(resolve)); }

test("served Explore filters on the server and opens a deep-linked span lazily", async () => {
  const run = { id: "run-one", project: "default", label: "needle run", status: "ok", started: "2026-10-01T00:00:00Z" };
  const routes = {
    "api/v1/capabilities": { mode: "local", write: false }, "api/v1/projects": ["default"],
    "api/v1/runs": { items: [run], limit: 50, offset: 0 },
    "api/v1/runs/run-one": { id: "run-one", spans: [{ id: "span-one", kind: "llm", name: "chat", status: "ok", duration: 0.2 }], findings: [] },
    "api/v1/runs/run-one/spans/span-one/contents": [{ role: "assistant", body: "safe output" }],
  };
  const app = harness("http://localhost/?view=explore", null, routes);
  await settle();
  assert.match(app.root.textContent, /Explore/);
  const search = find(app.root, (e) => e["aria-label"] === "Search runs");
  search.value = "needle";
  find(app.root, (e) => e.tagName === "BUTTON" && e.textContent === "Apply filters").trigger("click");
  await settle();
  assert.ok(app.requested.some((path) => path.includes("q=needle")), "query was not sent to the server");
  assert.ok(!app.requested.some((path) => path.endsWith("/contents")), "content loaded before span selection");
  find(app.root, (e) => e.tagName === "A" && e.textContent.includes("needle run")).trigger("click");
  await settle();
  find(app.root, (e) => e.tagName === "BUTTON" && e.textContent.includes("chat")).trigger("click");
  await settle();
  assert.match(app.location.href, /view=trace/);
  assert.match(app.location.href, /run=run-one/);
  assert.match(app.location.href, /span=span-one/);
  assert.ok(app.requested.some((path) => path.endsWith("/contents")), "selected span content was not loaded");
  assert.match(app.root.textContent, /safe output/);
});

test("standalone incident export opens its inlined trace without network access", async () => {
  const inline = { runs: [{ id: "r", label: "offline" }], run: { id: "r", spans: [{ id: "s", name: "agent", kind: "agent", status: "ok" }], findings: [], contents: { s: [{ role: "user", body: "offline body" }] } } };
  const app = harness("file:///tmp/report.html", inline, {});
  await settle();
  assert.match(app.root.textContent, /Trace/);
  assert.equal(app.requested.length, 0);
  find(app.root, (e) => e.tagName === "BUTTON" && e.textContent.includes("agent")).trigger("click");
  await settle();
  assert.match(app.root.textContent, /offline body/);
});

test("Overview uses full-window metrics and preserves investigation filters", async () => {
  const group = { key: "v2", runs: 200, errors: 10, running: 2, p95_latency_ms: 0,
    known_cost_usd: 2.5, unpriced_llm_spans: 3 };
  const app = harness("http://localhost/?view=overview&project=p&environment=prod&from=2026-10-18T00:00:00Z&to=2026-10-19T00:00:00Z", null, {
    "api/v1/capabilities": { mode: "local", write: false }, "api/v1/projects": ["p"],
    "api/v1/operations": { total: group, releases: [group], hourly: [{ ...group, key: "2026-10-18T12:00:00Z" }],
      quality: [{ release: "v2", name: "quality", average: 0.8, samples: 2, scored_runs: 2 }] },
    "api/v1/incidents": [], "api/v1/runs": { items: [], limit: 50, offset: 0 },
  });
  await settle();
  assert.match(app.root.textContent, /Runs in range200/);
  assert.match(app.root.textContent, /0ms/);
  assert.match(app.root.textContent, /0\.800/);
  assert.match(app.root.textContent, /unpriced spans can make it incomplete/);
  for (const name of ["operations?", "incidents?"]) {
    const path = app.requested.find((p) => p.includes(name));
    assert.ok(path, `missing ${name}`);
    const q = new URL(path, "http://localhost").searchParams;
    assert.equal(q.get("environment"), "prod");
    assert.equal(q.get("from"), "2026-10-18T00:00:00Z");
  }
  find(app.root, (e) => e.tagName === "A" && e.textContent === "v2").trigger("click");
  await settle();
  const q = new URL(app.location.href).searchParams;
  assert.equal(q.get("view"), "explore");
  assert.equal(q.get("release"), "v2");
  assert.equal(q.get("environment"), "prod");
  assert.equal(q.get("from"), "2026-10-18T00:00:00Z");
});

test("dataset editor saves corrected exact text and hides editing for frozen cases", async () => {
  const routes = {
    "api/v1/capabilities": { mode: "hub", project: "p", write: true }, "api/v1/projects": ["p"],
    "api/v1/datasets": [{ id: "d", name: "failures", frozen: false }],
    "api/v1/datasets/d/items": [{ id: "i", source_run_id: "source", input_json: '"question"', expected_json: '"wrong answer"' }],
    "api/v1/datasets/d/experiments": [], "api/v1/datasets/d/items/i/expected": {},
  };
  const app = harness("http://localhost/?view=datasets", null, routes);
  await settle();
  const answer = find(app.root, (e) => e.tagName === "TEXTAREA");
  assert.equal(answer.value, "wrong answer");
  answer.value = "corrected answer\n";
  const form = find(app.root, (e) => e.tagName === "FORM" && find(e, (c) => c.tagName === "BUTTON" && c.textContent === "Save expected output"));
  form.trigger("submit");
  await settle();
  const request = app.requests.find((r) => r.path.endsWith("/expected"));
  assert.equal(request.options.method, "POST");
  assert.equal(request.options.headers["X-Capybara-Write"], "1");
  assert.equal(JSON.parse(request.options.body).expected, "corrected answer\n");
  assert.match(app.status.textContent, /Expected output saved/);

  const frozen = harness("http://localhost/?view=datasets", null, { ...routes, "api/v1/datasets": [{ id: "d", name: "failures", frozen: true }] });
  await settle();
  assert.equal(find(frozen.root, (e) => e.tagName === "TEXTAREA"), null);
  assert.ok(find(frozen.root, (e) => e.tagName === "BUTTON" && e.textContent === "Clone dataset"));
  assert.match(frozen.root.textContent, /Cases are frozen/);
});

test("Trace loads linked evidence and prompt bodies only when opened", async () => {
  const routes = {
    "api/v1/capabilities": { mode: "local", write: false }, "api/v1/projects": ["default"],
    "api/v1/runs/r": { id: "r", spans: [{ id: "s", kind: "llm", name: "chat", cost: 0, duration: 0 }],
      findings: [{ span: "s", type: "drift", summary: "schema changed", detail: '{"missing":["field"]}' }],
      logs: [{ id: "log", span_id: "s", at: "2026-10-18T12:00:00Z" }] },
    "api/v1/runs/r/spans/s/contents": [],
    "api/v1/runs/r/logs": [{ ID: "log", SpanID: "s", Severity: "ERROR", Body: "late linked message", ObservedAt: "2026-10-18T12:00:00Z" }],
    "api/v1/runs/r/costs": [{ span_id: "s", kind: "provider", price_source: "receipt", price_version: "receipt-1", usage_source: "reported", checked_at: "2026-10-18T12:00:00Z" }],
    "api/v1/runs/r/prompts": [{ span_id: "s", sha256: "hash", prompt: { id: "p", name: "system", version: 2 } }],
    "api/v1/prompts/p": { content: "<script>recorded text</script>" },
    "api/v1/runs/r/scores": [{ name: "quality", value: 0.8, source: "human", comment: "reviewed" }],
  };
  const app = harness("http://localhost/?view=trace&run=r&span=s", null, routes);
  await settle();
  assert.match(app.root.textContent, /Detector signal: schema changed/);
  assert.match(app.root.textContent, /Confidence: not calibrated/);
  assert.ok(!app.requested.some((p) => /\/(logs|costs|prompts|scores)$/.test(p)), "supplementary evidence loaded eagerly");
  function open(label) {
    const details = find(app.root, (e) => e.tagName === "DETAILS" && e.children.some((c) => c.tagName === "SUMMARY" && c.textContent.startsWith(label)));
    assert.ok(details, `missing ${label}`); details.open = true; details.trigger("toggle");
  }
  open("Linked logs"); open("Cost sources"); open("Prompt versions"); open("Scores & feedback");
  await settle();
  assert.match(app.root.textContent, /late linked message/);
  assert.match(app.root.textContent, /\$0\.0000 · provider · receipt/);
  assert.match(app.root.textContent, /quality: 0\.8 · human/);
  assert.ok(!app.requested.includes("api/v1/prompts/p"), "prompt body loaded before explicit request");
  find(app.root, (e) => e.tagName === "BUTTON" && e.textContent === "Load prompt content").trigger("click");
  await settle();
  assert.match(app.root.textContent, /<script>recorded text<\/script>/);
  assert.equal(find(app.root, (e) => e.tagName === "SCRIPT"), null);
});

test("live updates preserve dataset drafts until an explicit refresh", async () => {
  const app = harness("http://localhost/?view=datasets", null, {
    "api/v1/capabilities": { mode: "hub", project: "p", write: true }, "api/v1/projects": ["p"],
    "api/v1/datasets": [{ id: "d", name: "cases", frozen: false }],
    "api/v1/datasets/d/items": [{ id: "i", source_run_id: "r", input_json: '"question"', expected_json: '"saved answer"' }],
    "api/v1/datasets/d/experiments": [],
  }, true);
  await settle();
  const draft = find(app.root, (e) => e.tagName === "TEXTAREA");
  draft.value = "unsaved corrected answer"; app.input(); app.emitUpdate();
  await new Promise((resolve) => setTimeout(resolve, 300));
  assert.equal(find(app.root, (e) => e.tagName === "TEXTAREA"), draft);
  assert.equal(draft.value, "unsaved corrected answer");
  assert.match(app.status.textContent, /Save your edits/);
  assert.equal(app.requested.filter((p) => p.startsWith("api/v1/datasets?")).length, 1);
  app.refresh.trigger("click"); await settle();
  assert.equal(find(app.root, (e) => e.tagName === "TEXTAREA").value, "saved answer");
  assert.equal(app.requested.filter((p) => p.startsWith("api/v1/datasets?")).length, 2);
});
