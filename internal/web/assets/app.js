// Read-only view. Recorded bodies are written with textContent only — never
// innerHTML — because a trace can contain anything the agent saw.
(function () {
  "use strict";

  var inline = window.CAPYBARA;
  var state = { run: null, span: null, collapsed: {} };

  function el(id) { return document.getElementById(id); }

  function tag(name, className, text) {
    var node = document.createElement(name);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function get(path) {
    return fetch(path).then(function (r) {
      if (!r.ok) throw new Error(path + ": " + r.status);
      return r.json();
    });
  }

  function loadRuns() {
    return inline ? Promise.resolve(inline.runs) : get("api/runs");
  }

  function loadRun(id) {
    if (inline) return Promise.resolve(inline.run);
    return get("api/runs/" + encodeURIComponent(id));
  }

  function shortID(id) { return id.length > 8 ? id.slice(0, 8) : id; }

  function duration(seconds) {
    if (!seconds) return "";
    return seconds < 1 ? Math.round(seconds * 1000) + "ms" : seconds.toFixed(1) + "s";
  }

  function money(cost) {
    return cost === undefined || cost === null ? "" : "$" + cost.toFixed(4);
  }

  // Status marks, the same alphabet the terminal view uses.
  function mark(status, findings) {
    if (status === "error") return { text: "x", cls: "mark err" };
    if (findings) return { text: "!", cls: "mark warn" };
    if (status === "running") return { text: ".", cls: "mark run" };
    return { text: " ", cls: "mark" };
  }

  // The status line carries what the terminal one does: the error first, then
  // findings, then the counts.
  function status(parts, cls) {
    var bar = el("status");
    bar.className = cls || "";
    bar.textContent = parts.filter(Boolean).join(" | ");
  }

  function count(n, noun) {
    return n + " " + noun + (n === 1 ? "" : "s");
  }

  function renderRuns(runs) {
    var list = el("run-list");
    list.replaceChildren();
    if (!runs.length) {
      list.append(tag("li", "empty", "no runs recorded"));
      return;
    }
    runs.forEach(function (run) {
      var item = tag("li");
      item.dataset.id = run.id;
      var m = mark(run.status, run.findings);
      var head = tag("span");
      head.append(tag("span", m.cls, m.text), " ", run.label || shortID(run.id));
      item.append(head);
      var bits = [run.model, duration(run.duration), money(run.cost)].filter(Boolean);
      if (run.findings) bits.push(count(run.findings, "finding"));
      item.append(tag("span", "meta", bits.join("  ")));
      item.addEventListener("click", function () { selectRun(run); });
      list.append(item);
    });
  }

  function selectRun(run) {
    Array.prototype.forEach.call(el("run-list").children, function (item) {
      item.classList.toggle("on", item.dataset.id === run.id);
    });
    el("run-head").textContent = [
      shortID(run.id), run.source, run.status, money(run.cost),
    ].filter(Boolean).join(" - ");
    el("tree-title").textContent = "trace " + shortID(run.id);
    el("span-detail").replaceChildren(tag("p", "empty", "select a span"));
    loadRun(run.id).then(function (detail) {
      state.run = detail;
      state.span = null;
      state.collapsed = {};
      renderTree();
      var findings = (detail.findings || []).length;
      status([
        findings ? count(findings, "finding") : "",
        count((detail.spans || []).length, "span"),
      ], findings ? "warn" : "");
    }).catch(fail);
  }

  function findingsBySpan() {
    var out = {};
    (state.run.findings || []).forEach(function (f) {
      (out[f.span] = out[f.span] || []).push(f);
    });
    return out;
  }

  function buildTree(spans) {
    var nodes = new Map();
    spans.forEach(function (span) { nodes.set(span.id, { span: span, children: [] }); });
    var roots = [];
    nodes.forEach(function (node) {
      var parent = nodes.get(node.span.parent);
      (parent ? parent.children : roots).push(node);
    });
    return roots;
  }

  function renderTree() {
    var host = el("span-tree");
    host.replaceChildren();
    var spans = state.run.spans || [];
    if (!spans.length) {
      host.append(tag("p", "empty", "no spans recorded"));
      return;
    }
    var found = findingsBySpan();
    buildTree(spans).forEach(function (node) { host.append(nodeView(node, found)); });
  }

  function nodeView(node, found) {
    var wrap = tag("div", "node");
    var span = node.span;
    var findings = found[span.id] || [];
    var row = tag("div", "row");
    row.dataset.id = span.id;

    var toggle = tag("button", "toggle", node.children.length ? (state.collapsed[span.id] ? "+" : "-") : "");
    toggle.addEventListener("click", function (event) {
      event.stopPropagation();
      state.collapsed[span.id] = !state.collapsed[span.id];
      renderTree();
    });

    var m = mark(span.status, findings.length);
    var label = span.tool || span.model || span.name;
    row.append(
      toggle,
      tag("span", m.cls, m.text),
      tag("span", "kind", span.kind),
      tag("span", "name", label),
      tag("span", "num", [duration(span.duration), money(span.cost)].filter(Boolean).join("  ")),
    );
    row.addEventListener("click", function () { selectSpan(span, findings); });
    wrap.append(row);

    findings.forEach(function (f) {
      wrap.append(tag("div", "note" + (f.severity === "error" ? " error" : ""), f.summary));
    });
    if (!state.collapsed[span.id]) {
      node.children.forEach(function (child) { wrap.append(nodeView(child, found)); });
    }
    return wrap;
  }

  function selectSpan(span, findings) {
    state.span = span.id;
    Array.prototype.forEach.call(document.querySelectorAll(".row"), function (row) {
      row.classList.toggle("on", row.dataset.id === span.id);
    });
    var host = el("span-detail");
    host.replaceChildren();
    host.append(tag("h3", null, span.tool || span.model || span.name));
    var info = [span.kind, span.status, duration(span.duration), money(span.cost)];
    if (span.tokens_in || span.tokens_out) {
      info.push("tok " + span.tokens_in + "/" + span.tokens_out);
    }
    host.append(tag("p", "info", info.filter(Boolean).join(" - ")));

    findings.forEach(function (f) {
      var line = f.summary.indexOf(f.type) === 0 ? f.summary : f.type + ": " + f.summary;
      host.append(tag("p", "finding" + (f.severity === "error" ? " error" : ""), line));
      if (f.detail) host.append(tag("pre", null, pretty(f.detail)));
    });

    var contents = (state.run.contents || {})[span.id] || [];
    if (!contents.length && !findings.length) {
      host.append(tag("p", "empty", "no content recorded"));
    }
    contents.forEach(function (c) {
      host.append(tag("h3", null, c.role));
      host.append(tag("pre", null, c.media === "application/json" ? pretty(c.body) : c.body));
    });
  }

  function pretty(body) {
    try {
      return JSON.stringify(JSON.parse(body), null, 2);
    } catch (err) {
      return body;
    }
  }

  function fail(err) {
    status([String(err)], "err");
  }

  loadRuns().then(function (runs) {
    renderRuns(runs);
    if (runs.length) {
      selectRun(runs[0]);
    } else {
      status(["no runs recorded"]);
    }
  }).catch(fail);
})();
