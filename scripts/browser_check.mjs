// Optional real-browser check; no frontend build or runtime dependency.
// Build capybara, then run with an installed Playwright module:
// CAPYBARA_PLAYWRIGHT=/path/to/playwright/index.mjs node scripts/browser_check.mjs /path/to/capybara
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const { chromium } = await import(process.env.CAPYBARA_PLAYWRIGHT || "playwright");
const binary = resolve(process.argv[2] || "./capybara");
const directory = mkdtempSync(join(tmpdir(), "capybara-browser-"));
const db = join(directory, "workspace.db");
const env = { ...process.env, XDG_CONFIG_HOME: directory };
const cli = (...args) => execFileSync(binary, ["-db", db, ...args], { encoding: "utf8", env }).trim();
const run = cli("sample");
const token = cli("hub", "token", "create", "-role", "editor").split(/\s+/)[1];
const dataset = cli("dataset", "create", "browser regressions");
const item = cli("dataset", "add", dataset, run);
const prompt = join(directory, "system.txt");
writeFileSync(prompt, "Answer with <script>window.__CAPYBARA_UNSAFE=true</script>");
cli("prompt", "register", "-name", "browser-system", prompt);

const server = spawn(binary, ["-db", db, "hub", "serve", "-addr", "127.0.0.1:0", "-capture-content"],
  { env, stdio: ["ignore", "pipe", "pipe"] });
let serverError = "";
server.stderr.on("data", (chunk) => { serverError = (serverError + chunk).slice(-65536); });
const exited = new Promise((resolveExit) => server.once("exit", resolveExit));
let browser;
try {
  const base = await new Promise((ready, reject) => {
    const timeout = setTimeout(() => reject(new Error("hub startup timed out: " + serverError)), 10000);
    let announced = "";
    server.stdout.on("data", (chunk) => {
      announced += chunk;
      const address = announced.split("\n").slice(0, -1).find((line) => line.startsWith("http://"));
      if (address) { clearTimeout(timeout); ready(address); }
    });
    server.once("error", (err) => { clearTimeout(timeout); reject(err); });
    server.once("exit", () => { clearTimeout(timeout); reject(new Error("hub exited: " + serverError)); });
  });
  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 },
    httpCredentials: { username: "browser-check", password: token } });
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  const errors = [];
  page.on("pageerror", (err) => errors.push(String(err)));
  await page.goto(base + "/?view=overview");
  await page.getByText("Runs in range", { exact: true }).waitFor();
  await page.screenshot({ path: join(directory, "overview-desktop.png"), fullPage: true });

  await page.goto(base + "/?view=datasets");
  await page.getByRole("heading", { name: "browser regressions", exact: true }).waitFor();
  await page.getByText("Case " + item.slice(0, 12), { exact: true }).click();
  const answer = page.getByRole("textbox", { name: "Expected output for case " + item.slice(0, 12) });
  await answer.fill("draft survives a live update");
  // The real hub emits updates every ten seconds; the form must survive one.
  await page.waitForTimeout(11000);
  assert.equal(await answer.inputValue(), "draft survives a live update");
  assert.match(await page.getByRole("status").innerText(), /Save your edits/);
  await answer.fill("corrected browser answer\n");
  await page.getByRole("button", { name: "Save expected output", exact: true }).click();
  await page.getByText("Expected output saved. Starting an experiment freezes these cases.", { exact: true }).waitFor();
  const response = await fetch(base + "/api/v1/datasets/" + dataset + "/items",
    { headers: { Authorization: "Bearer " + token } });
  assert.equal(response.status, 200);
  const saved = await response.json();
  assert.equal(JSON.parse(saved[0].expected_json), "corrected browser answer\n");
  await page.getByRole("textbox", { name: "Experiment name", exact: true }).fill("browser baseline");
  await page.getByRole("button", { name: "Create experiment", exact: true }).click();
  await page.getByText("Cases are frozen because this dataset has experiments. Clone it to revise expected outputs.", { exact: true }).waitFor();
  assert.equal(await page.locator("textarea").count(), 0);
  await page.getByRole("textbox", { name: "Clone name", exact: true }).fill("browser revised");
  await page.getByRole("button", { name: "Clone dataset", exact: true }).click();
  await page.getByRole("heading", { name: "browser revised", exact: true }).waitFor();

  await page.getByRole("link", { name: "Prompts", exact: true }).click();
  await page.getByRole("button", { name: "browser-system v1", exact: true }).click();
  await page.locator("pre").filter({ hasText: "window.__CAPYBARA_UNSAFE" }).waitFor();
  assert.equal(await page.evaluate(() => window.__CAPYBARA_UNSAFE), undefined);

  const trace = base + "/?view=trace&run=" + encodeURIComponent(run) + "&span=capybara-sample-tool";
  await page.goto(trace);
  await page.locator("pre").filter({ hasText: "price service unavailable" }).waitFor();
  await page.getByText("Linked logs (0)", { exact: true }).click();
  await page.getByText("No linked logs recorded.", { exact: true }).waitFor();
  await page.getByText("Cost sources", { exact: true }).click();
  await page.getByText("No cost provenance recorded. Existing amounts remain unattributed.", { exact: true }).waitFor();

  await page.setViewportSize({ width: 390, height: 844 });
  for (const [name, href, loaded] of [["overview-mobile", base + "/?view=overview", "Runs in range"],
    ["trace-mobile", trace, "lookup_price"]]) {
    await page.goto(href);
    if (name === "trace-mobile") await page.getByRole("button", { name: new RegExp(loaded + "$") }).waitFor();
    else await page.getByText(loaded, { exact: true }).waitFor();
    const size = await page.evaluate(() => ({ content: document.documentElement.scrollWidth, viewport: innerWidth }));
    assert.ok(size.content <= size.viewport + 1, name + " overflows horizontally: " + JSON.stringify(size));
    await page.screenshot({ path: join(directory, name + ".png"), fullPage: true });
  }
  assert.deepEqual(errors, [], "browser runtime errors");
  console.log(JSON.stringify({ passed: true, checks: ["overview", "live draft preservation", "expected output persistence",
    "dataset freeze and clone", "prompt escaping", "trace evidence", "mobile layout"], artifacts: directory }));
} catch (err) {
  const page = browser?.contexts()[0]?.pages()[0];
  if (page) await page.screenshot({ path: join(directory, "failure.png"), fullPage: true }).catch(() => {});
  console.error("Browser artifacts: " + directory);
  throw err;
} finally {
  if (browser) await browser.close();
  server.kill("SIGTERM");
  const stopped = await Promise.race([exited.then(() => true), new Promise((r) => { const t = setTimeout(() => r(false), 3000); t.unref(); })]);
  if (!stopped) { server.kill("SIGKILL"); await exited; }
}
