import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { chromium } from 'playwright';

const port = 4178;
const address = `http://127.0.0.1:${port}`;
const server = spawn(process.execPath, ['node_modules/vite/bin/vite.js', 'preview', '--host', '127.0.0.1', '--port', String(port), '--strictPort'], { stdio: 'ignore' });
let browser;

try {
  let ready = false;
  for (let i = 0; i < 60; i++) {
    try {
      const response = await fetch(address);
      ready = response.ok;
      if (ready) break;
    } catch { /* waiting for the preview server */ }
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  assert.ok(ready, 'preview server did not start; run npm run build first');

  browser = await chromium.launch({ headless: true });
  const desktop = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
  const errors = [];
  const externalRequests = [];
  desktop.on('pageerror', error => errors.push(error.message));
  desktop.on('request', request => { if (!request.url().startsWith(address)) externalRequests.push(request.url()); });
  await desktop.goto(address);
  assert.match(await desktop.title(), /Capybara/);
  assert.ok(await desktop.getByRole('heading', { name: /Find the step that broke/ }).isVisible());
  assert.ok(await desktop.locator('.hero-window img').evaluate(image => image.complete && image.naturalWidth > 0));
  assert.ok((await fetch(`${address}/assets/pupu-tour.mp4`, { method: 'HEAD' })).ok, 'TUI video is missing');
  assert.ok(await desktop.evaluate(() => [...document.querySelectorAll('a[href^="#"]')].every(link => document.getElementById(link.hash.slice(1)))), 'broken in-page link');

  await desktop.getByRole('button', { name: 'Play replay' }).click();
  await desktop.waitForTimeout(350);
  assert.ok(Number(await desktop.locator('#trace-progress').inputValue()) > 0, 'replay did not advance');
  await desktop.getByRole('button', { name: 'Pause replay' }).click();

  await desktop.getByRole('button', { name: /lookup_price/ }).click();
  assert.ok(await desktop.getByText('Error: pricing service timed out.').isVisible());
  await desktop.getByRole('button', { name: /answer 700ms/ }).click();
  assert.ok(await desktop.getByText(/current value of your 120 ACME shares/).isVisible());
  await desktop.getByRole('button', { name: /improvised/ }).click();
  assert.ok(await desktop.locator('.finding-strip').getByText(/answer after tool failure/).isVisible());
  assert.ok(await desktop.locator('.real-demo video source').getAttribute('src') === '/assets/pupu-tour.mp4');
  await desktop.getByRole('button', { name: 'Current source' }).click();
  assert.ok(await desktop.getByText('go install github.com/tonquoc0407/capybara/cmd/capybara@main').isVisible());
  await desktop.getByRole('button', { name: 'Homebrew' }).click();
  assert.ok(await desktop.getByText('brew install tonquoc0407/tap/capybara').isVisible());
  await desktop.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  await desktop.locator('.install-terminal .copy-command').first().click();
  assert.equal(await desktop.evaluate(() => navigator.clipboard.readText()), 'brew install tonquoc0407/tap/capybara');
  await desktop.getByRole('button', { name: 'HTTP events' }).click();
  assert.ok(await desktop.getByText('export CAPYBARA_EVENTS_URL=http://127.0.0.1:4318/api/v1/events').isVisible());
  assert.deepEqual(errors, [], `browser errors: ${errors.join(', ')}`);
  assert.deepEqual(externalRequests, [], `third-party requests: ${externalRequests.join(', ')}`);
  await desktop.screenshot({ path: '/tmp/capybara-landing-desktop.png', fullPage: true });

  const mobile = await browser.newPage({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 1 });
  mobile.on('pageerror', error => errors.push(error.message));
  await mobile.goto(address);
  assert.ok(await mobile.getByRole('heading', { name: /Find the step that broke/ }).isVisible());
  assert.ok(await mobile.getByRole('button', { name: 'Play replay' }).isVisible());
  assert.ok(await mobile.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'mobile page overflows horizontally');
  assert.deepEqual(errors, [], `browser errors: ${errors.join(', ')}`);
  await mobile.screenshot({ path: '/tmp/capybara-landing-mobile.png', fullPage: true });

  console.log('Desktop and mobile landing smoke test passed.');
} finally {
  await browser?.close();
  server.kill('SIGTERM');
}
