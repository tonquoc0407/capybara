// Recorded content is always inserted as text, never interpreted as HTML.
(function () {
  "use strict";
  var inline = window.CAPYBARA;
  var root = document.getElementById("workspace");
  var status = document.getElementById("status");
  var projectInput = document.getElementById("project");
  var generation = 0;
  var edited = false;
  var refreshButton = document.getElementById("refresh");
  var capabilities = { write: false, mode: "local" };

  function node(type, text, cls) {
    var e = document.createElement(type);
    if (text !== undefined) e.textContent = text;
    if (cls) e.className = cls;
    return e;
  }
  function add(parent, type, text, cls) { var e = node(type, text, cls); parent.append(e); return e; }
  function empty(parent, text) { add(parent, "p", text, "empty"); }
  function url() { return new URL(location.href); }
  function param(name) { return url().searchParams.get(name) || ""; }
  function project() { return param("project") || "default"; }
  function navigate(changes) {
    var u = url();
    Object.keys(changes).forEach(function (k) { changes[k] ? u.searchParams.set(k, changes[k]) : u.searchParams.delete(k); });
    history.pushState(null, "", u);
    render();
  }
  function endpoint(path) { return "api/v1/" + path; }
  function get(path) {
    return fetch(endpoint(path)).then(function (r) {
      if (!r.ok) throw new Error("Request failed (" + r.status + "): " + path);
      return r.json();
    });
  }
  function post(path, value) {
    return fetch(endpoint(path), { method: "POST", headers: { "Content-Type": "application/json", "X-Capybara-Write": "1" },
      body: JSON.stringify(value) }).then(function (r) {
      if (!r.ok) return r.text().then(function (body) { throw new Error(body || "Request failed (" + r.status + ")"); });
      return r.status === 204 ? null : r.json();
    });
  }
  function runs(offset) {
    if (inline) return Promise.resolve({ items: inline.runs || [], offset: 0, limit: 50 });
    var q = new URLSearchParams({ project: project(), limit: "50", offset: String(offset) });
    ["q", "environment", "release", "status", "from", "to"].forEach(function (key) {
      if (param(key)) q.set(key, param(key));
    });
    return get("runs?" + q.toString());
  }
  function detail(id) {
    return inline ? Promise.resolve(inline.run) : get("runs/" + encodeURIComponent(id));
  }
  function stamp(value) { return value ? new Date(value).toLocaleString() : "time unknown"; }
  function localTime(value) {
    var d = new Date(value);
    return Number.isNaN(d.getTime()) ? "" : new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  }
  function short(id) { return (id || "").slice(0, 12); }
  function money(value) { return value == null ? "cost unknown" : "$" + Number(value).toFixed(4); }
  function duration(value) { return value == null ? "—" : (value < 1 ? Math.round(value * 1000) + "ms" : value.toFixed(1) + "s"); }
  function message(text, error) { status.textContent = text; status.className = error ? "error" : ""; }
  function fail(err) { message(String(err), true); empty(root, String(err)); }
  function title(name, note) { add(root, "h1", name); if (note) add(root, "p", note, "muted"); }
  function card(parent, heading) { var c = add(parent, "section", undefined, "card"); if (heading) add(c, "h2", heading); return c; }
  function field(parent, label, value) {
    var wrap = add(parent, "label", label + " "); var input = add(wrap, "input");
    input.value = value || ""; input.setAttribute("aria-label", label); return input;
  }
  function link(parent, label, changes) {
    var a = add(parent, "a", label);
    var u = url(); Object.keys(changes).forEach(function (k) { changes[k] ? u.searchParams.set(k, changes[k]) : u.searchParams.delete(k); });
    a.href = u.pathname + u.search;
    a.addEventListener("click", function (e) { e.preventDefault(); navigate(changes); });
    return a;
  }
  function current(token) { return token === generation; }

  function table(parent, headers) {
    var t = add(add(parent, "div", undefined, "scroll"), "table");
    var row = add(add(t, "thead"), "tr");
    headers.forEach(function (header) { var th = add(row, "th", header); th.setAttribute("scope", "col"); });
    return add(t, "tbody");
  }
  function operationsRows(parent, rows, heading, hourly) {
    if (!rows.length) { empty(parent, "No runs in this time range."); return; }
    var body = table(parent, [heading, "Runs", "Errors", "Error rate", "p95 latency", "Known cost", "Unpriced LLM spans"]);
    rows.forEach(function (g) {
      var row = add(body, "tr"), name = add(row, "td");
      if (hourly) {
        var end = new Date(new Date(g.key).getTime() + 3600000).toISOString();
        link(name, stamp(g.key), { view: "explore", from: g.key, to: end, status: "", q: "", offset: "" });
      } else if (g.key) link(name, g.key, { view: "explore", release: g.key, status: "", q: "", offset: "" });
      else name.textContent = "Release not recorded";
      [g.runs, g.errors, g.runs ? (100 * g.errors / g.runs).toFixed(1) + "%" : "—",
        g.p95_latency_ms == null ? "—" : duration(g.p95_latency_ms / 1000), money(g.known_cost_usd), g.unpriced_llm_spans]
        .forEach(function (value) { add(row, "td", String(value)); });
    });
  }

  function overview(token) {
    title("Overview", "Observed runs and grouped investigation hints for " + project() + ".");
    if (inline) { empty(root, "Overview requires the live database."); return; }
    var bar = add(root, "form", undefined, "toolbar");
    var env = field(bar, "Environment", param("environment")), release = field(bar, "Release", param("release"));
    var from = field(bar, "From", ""), to = field(bar, "To", "");
    from.type = to.type = "datetime-local";
    var defaultEnd = new Date(), defaultStart = new Date(defaultEnd.getTime() - 86400000);
    var rangeFrom = param("from") || defaultStart.toISOString(), rangeTo = param("to") || defaultEnd.toISOString();
    if (!param("from") || !param("to")) {
      var u = url(); u.searchParams.set("from", rangeFrom); u.searchParams.set("to", rangeTo); history.replaceState(null, "", u);
    }
    from.value = localTime(rangeFrom); to.value = localTime(rangeTo);
    var apply = add(bar, "button", "Apply time range"); apply.type = "submit";
    bar.addEventListener("submit", function (e) {
      e.preventDefault();
      if (!from.value || !to.value) { message("Choose both ends of the time range", true); return; }
      navigate({ environment: env.value, release: release.value, from: new Date(from.value).toISOString(), to: new Date(to.value).toISOString(), offset: "" });
    });
    [1, 7].forEach(function (days) {
      var b = add(bar, "button", days === 1 ? "Last 24 hours" : "Last 7 days"); b.type = "button";
      b.addEventListener("click", function () { var end = new Date(); navigate({ from: new Date(end.getTime() - days*86400000).toISOString(), to: end.toISOString(), offset: "" }); });
    });
    var metrics = add(root, "div", undefined, "grid");
    var releases = card(root, "By release"), trend = card(root, "Hourly trend"), quality = card(root, "Quality by release");
    var incidents = card(root, "Incidents");
    var q = new URLSearchParams({ project: project(), from: rangeFrom, to: rangeTo });
    ["environment", "release"].forEach(function (key) { if (param(key)) q.set(key, param(key)); });
    Promise.all([get("operations?" + q.toString()), get("incidents?" + q.toString() + "&limit=100")]).then(function (data) {
      if (!current(token)) return;
      var report = data[0], total = report.total, groups = data[1] || [];
      [["Runs in range", total.runs], ["Errors", total.errors], ["Running", total.running],
        ["p95 completed latency", total.p95_latency_ms == null ? "—" : duration(total.p95_latency_ms / 1000)],
        ["Known cost", money(total.known_cost_usd)], ["Unpriced LLM spans", total.unpriced_llm_spans]].forEach(function (entry) {
        var c = card(metrics, entry[0]); add(c, "div", String(entry[1]), "metric");
      });
      add(releases, "p", "Latency excludes running runs and invalid timestamps. Cost includes known amounts; unpriced spans can make it incomplete.", "meta");
      operationsRows(releases, report.releases || [], "Release", false);
      operationsRows(trend, report.hourly || [], "Hour", true);
      var qualityRows = report.quality || [];
      if (!qualityRows.length) empty(quality, "No scores recorded for these runs.");
      else {
        add(quality, "p", "Latest score per run, case and metric. Metrics remain separate; these averages do not establish causality or replace paired experiments.", "meta");
        var body = table(quality, ["Release", "Metric", "Average", "Samples", "Scored runs"]);
        qualityRows.forEach(function (score) { var row = add(body, "tr");
          [score.release || "Not recorded", score.name, score.average.toFixed(3), score.samples, score.scored_runs].forEach(function (value) { add(row, "td", String(value)); }); });
      }
      if (!total.runs) {
        var start = card(root, "Getting started");
        if (capabilities.mode !== "hub") add(start, "p", "Run capybara sample to load a labelled example trace into the database.");
        add(start, "p", capabilities.ingest ? "OTLP HTTP receiver: /v1/traces (project token required)." : "This read-only Web process does not receive OTLP; start the local capybara receiver to capture live traces.", "meta");
      }
      add(incidents, "p", "Groups from the latest 100 matching runs with errors or detector findings.", "meta");
      if (!groups.length) { empty(incidents, "No errors or detector findings in this time range."); message(total.runs + " runs in range"); return; }
      var list = add(incidents, "ul", undefined, "list");
      groups.forEach(function (g) {
        var li = add(list, "li");
        link(li, (g.first_error || "Detector finding") + " · " + g.count + " sampled run(s) · " + g.fingerprint, { view: "trace", run: g.runs[0], span: "" });
        add(li, "div", "Grouping is a deterministic hint; inspect the trace for cause.", "meta");
      });
      message(total.runs + " runs in range · " + groups.length + " incident groups");
    }).catch(fail);
  }

  function runRow(list, r) {
    var li = add(list, "li");
    link(li, (r.status === "error" ? "✕ " : r.findings ? "! " : "") + (r.label || short(r.id)), { view: "trace", run: r.id, span: "" });
    add(li, "div", [r.environment || "local", r.release, stamp(r.started), r.model, duration(r.duration), money(r.cost)].filter(Boolean).join(" · "), "meta");
  }
  function explore(token) {
    title("Explore", "Find a run, then open its trace.");
    var bar = add(root, "div", undefined, "toolbar");
    add(bar, "label", "Search "); var search = add(bar, "input"); search.value = param("q"); search.setAttribute("aria-label", "Search runs");
    add(bar, "label", "Environment "); var env = add(bar, "input"); env.value = param("environment"); env.setAttribute("aria-label", "Environment");
    add(bar, "label", "Release "); var release = add(bar, "input"); release.value = param("release"); release.setAttribute("aria-label", "Release");
    add(bar, "label", "Status "); var statusFilter = add(bar, "select"); statusFilter.setAttribute("aria-label", "Status");
    ["", "ok", "error", "running"].forEach(function (value) { var option = add(statusFilter, "option", value || "Any"); option.value = value; });
    statusFilter.value = param("status");
    add(bar, "label", "From "); var from = add(bar, "input"); from.type = "datetime-local"; from.setAttribute("aria-label", "From");
    if (param("from")) from.value = localTime(param("from"));
    add(bar, "label", "To "); var to = add(bar, "input"); to.type = "datetime-local"; to.setAttribute("aria-label", "To");
    if (param("to")) to.value = localTime(param("to"));
    var apply = add(bar, "button", "Apply filters");
    apply.addEventListener("click", function () { navigate({ q: search.value, environment: env.value, release: release.value,
      status: statusFilter.value, from: from.value ? new Date(from.value).toISOString() : "",
      to: to.value ? new Date(to.value).toISOString() : "", offset: "" }); });
    var results = card(root, "Runs"); var list = add(results, "ul", undefined, "list");
    var offset = Math.max(0, Number(param("offset")) || 0);
    runs(offset).then(function (page) {
      if (!current(token)) return;
      var items = page.items || [];
      if (!items.length) empty(list, "No matching runs. Change filters or open another page.");
      items.forEach(function (r) { runRow(list, r); });
      var pager = add(results, "div", undefined, "toolbar");
      if (offset) link(pager, "← Previous", { offset: String(Math.max(0, offset - 50)) });
      if (!inline && (page.items || []).length === 50) link(pager, "Next →", { offset: String(offset + 50) });
      message("Showing " + items.length + " of " + (page.items || []).length + " runs on this page");
    }).catch(fail);
  }

  function treeNodes(spans) {
    var byID = {}, roots = [];
    spans.forEach(function (s) { byID[s.id] = { span: s, children: [] }; });
    spans.forEach(function (s) { var n = byID[s.id]; (byID[s.parent] && s.parent !== s.id ? byID[s.parent].children : roots).push(n); });
    return roots;
  }
  function pretty(body) { try { return JSON.stringify(JSON.parse(body), null, 2); } catch (_) { return body; } }
  function detectorSignal(parent, f) {
    var host = add(parent, "details", undefined, "finding");
    add(host, "summary", "Detector signal: " + f.summary);
    add(host, "p", "Type: " + f.type + " · Confidence: not calibrated. This signal does not establish the root cause.", "meta");
    if (f.detail) add(host, "pre", pretty(f.detail));
  }
  function lazyEvidence(parent, heading, load, show, token) {
    var host = add(parent, "details"); add(host, "summary", heading);
    var body = add(host, "div"), loaded = false, loading = false;
    host.addEventListener("toggle", function () {
      if (!host.open || loaded || loading) return;
      loading = true; body.replaceChildren(); empty(body, "Loading…");
      Promise.resolve().then(load).then(function (rows) {
        if (!current(token)) return;
        body.replaceChildren(); show(body, rows || []); loaded = true;
      }).catch(function (err) {
        if (current(token)) { body.replaceChildren(); empty(body, String(err) + ". Close and reopen to retry."); }
      }).finally(function () { loading = false; });
    });
  }
  function traceEvidence(parent, d, id, token) {
    var base = "runs/" + encodeURIComponent(id), spans = {};
    (d.spans || []).forEach(function (s) { spans[s.id] = s; });
    lazyEvidence(parent, "Linked logs (" + (d.logs || []).length + ")", function () {
      return inline ? (d.logs || []) : get(base + "/logs");
    }, function (body, rows) {
      if (!rows.length) { empty(body, "No linked logs recorded."); return; }
      rows.forEach(function (log) {
        var line = add(body, "section");
        var spanID = log.span_id || log.SpanID, at = log.at || log.ObservedAt, severity = log.severity || log.Severity || "severity unknown";
        add(line, "p", stamp(at) + " · " + severity, severity === "ERROR" || severity === "FATAL" ? "error" : "meta");
        if (spanID && spans[spanID]) link(line, "Open span " + short(spanID), { span: spanID });
        else if (spanID) add(line, "p", "Span " + short(spanID) + " has not been recorded.", "meta");
        var text = log.body || log.Body, attrs = log.attrs_json || log.AttrsJSON;
        if (text) add(line, "pre", pretty(text)); else empty(line, "Log body is empty or was not recorded.");
        if (attrs && attrs !== "{}") add(line, "pre", pretty(attrs));
      });
    }, token);
    if (inline) return;
    lazyEvidence(parent, "Cost sources", function () { return get(base + "/costs"); }, function (body, rows) {
      add(body, "p", "Cost is estimated unless reported by the provider. Partial or missing usage can make it incomplete.", "meta");
      if (!rows.length) { empty(body, "No cost provenance recorded. Existing amounts remain unattributed."); return; }
      rows.forEach(function (cost) {
        var line = add(body, "section"), sp = spans[cost.span_id];
        link(line, (sp && (sp.model || sp.name)) || short(cost.span_id), { span: cost.span_id });
        add(line, "p", money(sp && sp.cost) + " · " + cost.kind + " · " + cost.price_source);
        add(line, "p", "Price version: " + cost.price_version + " · Usage: " + cost.usage_source, "meta");
        add(line, "p", "Price effective: " + (cost.price_effective_at ? stamp(cost.price_effective_at) : "not published") + " · Checked: " + stamp(cost.checked_at), "meta");
      });
    }, token);
    lazyEvidence(parent, "Prompt versions", function () { return get(base + "/prompts"); }, function (body, rows) {
      if (!rows.length) { empty(body, "No prompt hash recorded on these spans."); return; }
      rows.forEach(function (ref) {
        var line = add(body, "section");
        link(line, "Span " + short(ref.span_id), { span: ref.span_id });
        add(line, "p", "SHA-256: " + ref.sha256, "meta");
        if (!ref.prompt) { empty(line, "This hash has no registered prompt version in the project."); return; }
        add(line, "p", ref.prompt.name + " v" + ref.prompt.version);
        var button = add(line, "button", "Load prompt content");
        button.addEventListener("click", function () {
          button.disabled = true;
          get("prompts/" + encodeURIComponent(ref.prompt.id)).then(function (prompt) {
            if (current(token)) { add(line, "pre", prompt.content); button.textContent = "Prompt loaded"; }
          }).catch(function (err) { if (current(token)) { message(String(err), true); button.disabled = false; } });
        });
      });
    }, token);
    lazyEvidence(parent, "Scores & feedback", function () { return get(base + "/scores"); }, function (body, rows) {
      if (!rows.length) { empty(body, "No scores recorded."); return; }
      rows.forEach(function (score) {
        var line = add(body, "section"); add(line, "p", score.name + ": " + score.value + " · " + score.source);
        add(line, "p", stamp(score.created_at) + (score.item_id ? " · Case " + short(score.item_id) : ""), "meta");
        if (score.comment) add(line, "pre", score.comment);
      });
    }, token);
  }
  function trace(token) {
    var id = param("run") || (inline && inline.run && inline.run.id);
    title("Trace", id ? "Run " + id : "Choose a run in Explore.");
    if (!id) { link(root, "Browse runs", { view: "explore" }); return; }
    if (!inline) { var packetLink = add(root, "a", "Open incident packet (HTML)"); packetLink.href = endpoint("runs/" + encodeURIComponent(id) + "/incident-packet.html"); }
    var layout = add(root, "div", undefined, "trace"); var tree = card(layout, "Execution"); var evidence = card(layout, "Evidence");
    empty(tree, "Loading trace…"); empty(evidence, "Select a span to load its content.");
    detail(id).then(function (d) {
      if (!current(token)) return;
      tree.replaceChildren(); add(tree, "h2", "Execution");
      if (!(d.spans || []).length) empty(tree, "No spans recorded.");
      var selected = param("span");
      function openSpan(s) {
        navigate({ span: s.id });
      }
      var rendered = {};
      function branch(n, parent, rootNode) {
        if (rendered[n.span.id]) return;
        rendered[n.span.id] = true;
        var host = add(parent, "div", undefined, rootNode ? "tree-node tree-root" : "tree-node");
        var row = add(host, "div", undefined, "row");
        var b = add(row, "button", (n.span.status === "error" ? "✕ " : "") + (n.span.tool || n.span.model || n.span.name));
        if (n.span.id === selected) b.className = "selected";
        b.addEventListener("click", function () { openSpan(n.span); });
        add(row, "span", n.span.kind + " · " + duration(n.span.duration) + " · " + money(n.span.cost), "meta");
        n.children.forEach(function (child) { branch(child, host, false); });
      }
      var roots = treeNodes(d.spans || []);
      roots.forEach(function (n) { branch(n, tree, true); });
      // Malformed parent cycles have no root; keep their spans inspectable.
      var indexed = {};
      function index(n) { indexed[n.span.id] = n; n.children.forEach(function (c) { if (!indexed[c.span.id]) index(c); }); }
      roots.forEach(index);
      (d.spans || []).forEach(function (s) {
        if (!rendered[s.id]) branch(indexed[s.id] || { span: s, children: [] }, tree, true);
      });
      var runFindings = (d.findings || []).filter(function (f) { return !f.span; });
      runFindings.forEach(function (f) { detectorSignal(tree, f); });
      traceEvidence(card(root, "Run evidence"), d, id, token);
      if (capabilities.write) {
        var actions = card(root, "Next step");
        link(actions, "Use this run as a dataset case", { view: "datasets", run: id, span: "" });
        var form = add(actions, "form", undefined, "toolbar");
        var metric = field(form, "Score name", "quality"); var value = field(form, "Score 0–1", "1"); value.type = "number"; value.min = "0"; value.max = "1"; value.step = "0.01";
        var submit = add(form, "button", "Save score"); submit.type = "submit";
        form.addEventListener("submit", function (e) { e.preventDefault(); post("runs/" + encodeURIComponent(id) + "/scores", { name: metric.value, value: Number(value.value), source: "human" })
          .then(function () { message("Score saved"); }).catch(function (err) { message(String(err), true); }); });
      }
      if (!selected) { message((d.spans || []).length + " spans · " + (d.findings || []).length + " findings"); return; }
      var span = (d.spans || []).find(function (s) { return s.id === selected; });
      evidence.replaceChildren(); add(evidence, "h2", "Evidence");
      if (!span) { empty(evidence, "Span not found in this run."); return; }
      add(evidence, "h3", span.tool || span.model || span.name);
      add(evidence, "p", [span.kind, span.status, duration(span.duration), money(span.cost)].join(" · "), "meta");
      (d.findings || []).filter(function (f) { return f.span === selected; }).forEach(function (f) {
        detectorSignal(evidence, f);
      });
      var content = inline ? Promise.resolve((d.contents || {})[selected] || []) : get("runs/" + encodeURIComponent(id) + "/spans/" + encodeURIComponent(selected) + "/contents");
      content.then(function (rows) {
        if (!current(token)) return;
        if (!rows || !rows.length) empty(evidence, "No content recorded. Replay needs recorded input and an entrypoint.");
        (rows || []).forEach(function (c) { add(evidence, "h3", c.role); add(evidence, "pre", pretty(c.body)); });
      }).catch(function (err) { if (current(token)) empty(evidence, String(err)); });
      message((d.spans || []).length + " spans · " + (d.findings || []).length + " findings");
    }).catch(fail);
  }

  function datasets(token) {
    title("Datasets & experiments", "Cases captured from traces. Experiments compare recorded scores over the same cases.");
    var host = card(root, "Datasets");
    if (inline) { empty(host, "Dataset workspace requires the live database."); return; }
    if (capabilities.write) {
      var create = add(host, "form", undefined, "toolbar");
      var name = field(create, "New dataset", ""); var createButton = add(create, "button", "Create"); createButton.type = "submit";
      create.addEventListener("submit", function (e) { e.preventDefault(); post("datasets", { name: name.value })
        .then(function () { render(); }).catch(function (err) { message(String(err), true); }); });
    }
    get("datasets?project=" + encodeURIComponent(project())).then(function (data) {
      if (!current(token)) return;
      if (!data || !data.length) { empty(host, "No datasets. Use capybara dataset create, then add a run with recorded input and output."); return; }
      data.forEach(function (d) {
        var section = add(host, "section"); add(section, "h3", d.name);
        add(section, "p", short(d.id) + " · " + stamp(d.created_at), "meta");
        add(section, "p", d.frozen ? "Cases are frozen because this dataset has experiments. Clone it to revise expected outputs." : "Review expected outputs before starting experiments; captured failures may contain incorrect answers.", "meta");
        if (capabilities.write) {
          var cloneForm = add(section, "form", undefined, "toolbar"); var cloneName = field(cloneForm, "Clone name", d.name + " revised");
          var cloneButton = add(cloneForm, "button", "Clone dataset"); cloneButton.type = "submit";
          cloneForm.addEventListener("submit", function (e) { e.preventDefault(); post("datasets/" + encodeURIComponent(d.id) + "/clone", { name: cloneName.value }).then(render).catch(function (err) { message(String(err), true); }); });
          if (!d.frozen) {
          var addForm = add(section, "form", undefined, "toolbar"); var run = field(addForm, "Run ID", param("run"));
          var addButton = add(addForm, "button", "Add case"); addButton.type = "submit";
          addForm.addEventListener("submit", function (e) { e.preventDefault(); post("datasets/" + encodeURIComponent(d.id) + "/items", { run_id: run.value })
            .then(render).catch(function (err) { message(String(err), true); }); });
          }
          var experimentForm = add(section, "form", undefined, "toolbar"); var experimentName = field(experimentForm, "Experiment name", "");
          var experimentButton = add(experimentForm, "button", "Create experiment"); experimentButton.type = "submit";
          experimentForm.addEventListener("submit", function (e) { e.preventDefault(); post("experiments", { dataset_id: d.id, name: experimentName.value })
            .then(render).catch(function (err) { message(String(err), true); }); });
        }
        Promise.all([get("datasets/" + encodeURIComponent(d.id) + "/items"), get("datasets/" + encodeURIComponent(d.id) + "/experiments")]).then(function (pair) {
          if (!current(token)) return;
          add(section, "p", (pair[0] || []).length + " cases · " + (pair[1] || []).length + " experiments");
          (pair[0] || []).forEach(function (item) {
            var review = add(section, "details"); add(review, "summary", "Case " + short(item.id));
            link(review, "Open source " + short(item.source_run_id), { view: "trace", run: item.source_run_id, span: "" });
            add(review, "h3", "Recorded input"); add(review, "pre", pretty(item.input_json));
            var expected;
            try { expected = JSON.parse(item.expected_json); } catch (_) { expected = item.expected_json; }
            if (capabilities.write && !d.frozen) {
              var form = add(review, "form"); var label = add(form, "label", "Expected output (exact text)");
              var answer = add(label, "textarea"); answer.value = expected; answer.setAttribute("aria-label", "Expected output for case " + short(item.id));
              var save = add(form, "button", "Save expected output"); save.type = "submit";
              form.addEventListener("submit", function (e) { e.preventDefault(); post("datasets/" + encodeURIComponent(d.id) + "/items/" + encodeURIComponent(item.id) + "/expected", { expected: answer.value })
                .then(function () { message("Expected output saved. Starting an experiment freezes these cases."); }).catch(function (err) { message(String(err), true); }); });
            } else { add(review, "h3", "Expected output"); add(review, "pre", expected); }
          });
          (pair[1] || []).forEach(function (experiment) {
            add(section, "p", experiment.name + " · " + short(experiment.id), "meta");
            if (capabilities.write && pair[0].length) {
              var record = add(section, "form", undefined, "toolbar");
              var itemSelect = add(record, "select"); itemSelect.setAttribute("aria-label", "Dataset case");
              pair[0].forEach(function (item) { var option = add(itemSelect, "option", short(item.id)); option.value = item.id; });
              var resultRun = field(record, "Result run", ""); var scoreName = field(record, "Score name", "quality");
              var recordButton = add(record, "button", "Record in " + experiment.name); recordButton.type = "submit";
              record.addEventListener("submit", function (e) { e.preventDefault(); post("experiments/" + encodeURIComponent(experiment.id) + "/results",
                { item_id: itemSelect.value, run_id: resultRun.value, score_name: scoreName.value })
                .then(function () { message("Result recorded"); }).catch(function (err) { message(String(err), true); }); });
            }
          });
          if ((pair[1] || []).length >= 2) {
            var compare = add(section, "form", undefined, "toolbar");
            var baseline = add(compare, "select"); baseline.setAttribute("aria-label", "Baseline experiment");
            var candidate = add(compare, "select"); candidate.setAttribute("aria-label", "Candidate experiment");
            pair[1].forEach(function (e) {
              var left = add(baseline, "option", e.name); left.value = e.id;
              var right = add(candidate, "option", e.name); right.value = e.id;
            });
            candidate.selectedIndex = 1;
            var compareScore = field(compare, "Metric", "quality");
            var compareButton = add(compare, "button", "Compare"); compareButton.type = "submit";
            var comparison = add(section, "p", "", "meta");
            compare.addEventListener("submit", function (event) { event.preventDefault();
              get("experiments/compare?baseline=" + encodeURIComponent(baseline.value) + "&candidate=" + encodeURIComponent(candidate.value) + "&score=" + encodeURIComponent(compareScore.value))
                .then(function (result) { comparison.textContent = "Baseline " + result.baseline.toFixed(3) + " · Candidate " + result.candidate.toFixed(3) + " · Delta " + result.delta.toFixed(3) + " across " + result.items + " cases"; })
                .catch(function (err) { comparison.textContent = String(err); });
            });
          }
        }).catch(function (err) { if (current(token)) add(section, "p", String(err), "error"); });
      });
      message(data.length + " datasets");
    }).catch(fail);
  }

  function prompts(token) {
    title("Prompts", "File versions linked to traces by SHA-256.");
    var host = card(root, "Versions");
    if (inline) { empty(host, "Prompt versions require the live database."); return; }
    if (capabilities.write) {
      var form = add(host, "form", undefined, "toolbar"); var name = field(form, "Prompt name", "");
      var file = add(form, "input"); file.type = "file"; file.setAttribute("aria-label", "Prompt file");
      var button = add(form, "button", "Register file"); button.type = "submit";
      form.addEventListener("submit", function (e) { e.preventDefault();
        if (!file.files.length) { message("Choose a prompt file", true); return; }
        file.files[0].text().then(function (content) { return post("prompts", { name: name.value, content: content, source_path: file.files[0].name }); })
          .then(render).catch(function (err) { message(String(err), true); }); });
    }
    get("prompts?project=" + encodeURIComponent(project())).then(function (rows) {
      if (!current(token)) return;
      if (!rows || !rows.length) { empty(host, "No prompt versions. Register a prompt file with capybara prompt register."); return; }
      var list = add(host, "ul", undefined, "list");
      rows.forEach(function (p) {
        var li = add(list, "li");
        var b = add(li, "button", p.name + " v" + p.version); b.addEventListener("click", function () {
          get("prompts/" + encodeURIComponent(p.id)).then(function (version) { var pre = li.querySelector("pre"); if (pre) pre.remove(); add(li, "pre", version.content); }).catch(fail);
        });
        add(li, "div", p.sha256 + " · " + stamp(p.created_at), "meta");
      });
      message(rows.length + " prompt versions");
    }).catch(fail);
  }

  function usage(token) {
    title("Usage", "Cost is an estimate unless the provider reports it. Unknown prices remain unpriced.");
    var host = card(root, "By project");
    if (inline) { empty(host, "Usage summary requires the live database."); return; }
    get("usage").then(function (rows) {
      if (!current(token)) return;
      rows = (rows || []).filter(function (r) { return r.project === project(); });
      if (!rows.length) { empty(host, "No usage recorded for this project."); return; }
      rows.forEach(function (u) {
        var grid = add(host, "div", undefined, "grid");
        [["Runs", u.runs], ["Input tokens", u.tokens_in], ["Output tokens", u.tokens_out], ["Estimated", money(u.estimated_usd)], ["Provider reported", money(u.provider_usd)], ["Unpriced LLM spans", u.unpriced_llm_spans]].forEach(function (entry) {
          var c = card(grid, entry[0]); add(c, "div", String(entry[1]), "metric");
        });
      });
      message(rows.length + " project usage summaries");
    }).catch(fail);
  }

  function settings() {
    title("Settings", "Current workspace access and storage limits.");
    var c = card(root, "Connection");
    add(c, "p", "Mode: " + capabilities.mode);
    if (capabilities.role) add(c, "p", "Role: " + capabilities.role);
    add(c, "p", "Project: " + project());
    add(c, "p", "Content capture: " + String(capabilities.content_capture));
    if (capabilities.mode === "hub") {
      add(c, "p", "Retention: " + (capabilities.retention_hours || "off") + (capabilities.retention_hours ? " hours" : ""));
      add(c, "p", "Single-node SQLite hub. Remote access requires a TLS reverse proxy.", "muted");
    }
  }

  function render() {
    var token = ++generation, view = param("view") || (inline ? "trace" : "overview");
    edited = false;
    if (refreshButton) refreshButton.textContent = "Refresh";
    projectInput.value = project(); root.replaceChildren(); message("Loading…");
    document.querySelectorAll("nav a").forEach(function (a) {
      a.classList.toggle("active", a.dataset.view === view);
      var u = url(); u.searchParams.set("view", a.dataset.view); u.searchParams.delete("run"); u.searchParams.delete("span");
      a.href = u.pathname + u.search;
    });
    ({ overview: overview, explore: explore, trace: trace, datasets: datasets, prompts: prompts, usage: usage, settings: settings }[view] || overview)(token);
  }
  document.querySelectorAll("nav a").forEach(function (a) {
    a.addEventListener("click", function (e) { e.preventDefault(); navigate({ view: a.dataset.view }); });
  });
  projectInput.addEventListener("change", function () { navigate({ project: projectInput.value.trim() || "default", run: "", span: "", offset: "" }); });
  if (refreshButton) refreshButton.addEventListener("click", render);
  document.addEventListener("input", function () { edited = true; });
  window.addEventListener("popstate", render);
  document.addEventListener("keydown", function (e) {
    if (e.key === "/" && !["INPUT", "TEXTAREA"].includes(document.activeElement.tagName)) {
      var field = root.querySelector('input[aria-label="Search runs"]');
      if (field) { e.preventDefault(); field.focus(); }
    }
  });
  if (!inline && typeof EventSource !== "undefined") {
    var events = new EventSource("api/events");
    var refreshTimer;
    events.addEventListener("update", function () {
      if (document.hidden) return;
      clearTimeout(refreshTimer);
      refreshTimer = setTimeout(function () {
        var view = param("view") || "overview";
        if (edited || (view !== "overview" && view !== "explore") || ["INPUT", "TEXTAREA", "SELECT"].includes(document.activeElement.tagName)) {
          message(edited ? "New data available. Save your edits before using Refresh." : "New data available. Use Refresh to update this view.");
          if (refreshButton) refreshButton.textContent = "Refresh available";
          return;
        }
        render();
      }, 250);
    });
  }
  if (!inline) {
    get("capabilities").then(function (data) {
      capabilities = data;
      get("projects").then(function (projects) {
        var list = document.getElementById("projects"); list.replaceChildren();
        (projects || []).forEach(function (project) { var option = add(list, "option"); option.value = project; });
      }).catch(function () {});
      if (data.mode === "hub" && data.project) {
        var u = url(); u.searchParams.set("project", data.project);
        history.replaceState(null, "", u);
        projectInput.readOnly = true;
        projectInput.title = "This token is scoped to one project";
      }
      render();
    }).catch(render);
  } else {
    render();
  }
})();
