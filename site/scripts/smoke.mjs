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
  assert.ok(await desktop.locator('.hero-shot img').evaluate(image => image.complete && image.naturalWidth > 0));
  assert.ok((await fetch(`${address}/assets/pupu-tour.mp4`, { method: 'HEAD' })).ok, 'TUI video is missing');
  assert.ok(await desktop.evaluate(() => [...document.querySelectorAll('a[href^="#"]')].every(link => document.getElementById(link.hash.slice(1)))), 'broken in-page link');
  assert.ok(await desktop.locator('.marquee-copy').evaluateAll(copies => copies.length === 2 && copies[0].textContent === copies[1].textContent && copies[0].getBoundingClientRect().width >= window.innerWidth), 'marquee copies do not form a seamless loop');
  assert.equal(await desktop.locator('.gradient-text').count(), 1);
  assert.equal(await desktop.locator('.star-border, .glare-hover').count(), 0);
  assert.equal(await desktop.locator('.feature-card').count(), 3);

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
  assert.ok(await desktop.locator('.recording video source').getAttribute('src') === '/assets/pupu-tour.mp4');
  assert.ok(await desktop.locator('.recording video').evaluate(video => video.autoplay && video.loop && video.muted && !video.controls), 'video must autoplay without native progress controls');
  await desktop.locator('.recording video').scrollIntoViewIfNeeded();
  await desktop.waitForFunction(() => document.querySelector('.recording video')?.currentTime > 0.2, undefined, { timeout: 5000 });
  assert.ok((await fetch(`${address}/assets/agents-issue-4070.mp4`, { method: 'HEAD' })).ok, 'public case video is missing');
  assert.ok(await desktop.locator('.case-media video').evaluate(video => video.autoplay && video.loop && video.muted && !video.controls), 'public case video must autoplay without native progress controls');
  await desktop.locator('.case-media video').scrollIntoViewIfNeeded();
  await desktop.waitForFunction(() => document.querySelector('.case-media video')?.currentTime >= 6, undefined, { timeout: 5000 });
  assert.equal(await desktop.locator('.source-row').count(), 6);
  assert.ok(await desktop.getByRole('link', { name: 'Start beta test' }).isVisible());
  assert.ok(await desktop.getByText('The model span failed, but the regular agent span said').isVisible());
  for (const [tab, filename] of [['Web overview', 'web-overview.png'], ['Web trace', 'web-trace-mobile.png'], ['Terminal', 'pupu-workspace.png']]) {
    await desktop.getByRole('group', { name: 'Choose interface preview' }).getByRole('button', { name: tab }).click();
    await desktop.locator('.showcase-stage img').evaluate(image => image.decode());
    assert.ok(await desktop.locator('.showcase-stage img').evaluate((image, expected) => image.complete && image.naturalWidth > 0 && image.src.endsWith(expected), filename), `missing ${filename}`);
  }
  await desktop.getByRole('button', { name: 'Current source' }).click();
  assert.ok(await desktop.getByText('go install github.com/tonquoc0407/capybara/cmd/capybara@main').isVisible());
  await desktop.getByRole('button', { name: 'Homebrew' }).click();
  assert.ok(await desktop.getByText('brew install tonquoc0407/tap/capybara').isVisible());
  await desktop.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  await desktop.locator('.install-terminal .copy-command').first().click();
  assert.equal(await desktop.evaluate(() => navigator.clipboard.readText()), 'brew install tonquoc0407/tap/capybara');
  await desktop.getByRole('button', { name: 'HTTP events' }).click();
  assert.ok(await desktop.getByText('python examples/local_events.py').isVisible());
  assert.deepEqual(errors, [], `browser errors: ${errors.join(', ')}`);
  assert.deepEqual(externalRequests, [], `third-party requests: ${externalRequests.join(', ')}`);
  for (const element of await desktop.locator('[data-reveal]').all()) await element.scrollIntoViewIfNeeded();
  await desktop.waitForTimeout(750);
  await desktop.screenshot({ path: '/tmp/capybara-landing-desktop.png', fullPage: true });

  const wide = await browser.newPage({ viewport: { width: 2048, height: 1152 }, deviceScaleFactor: 1 });
  await wide.goto(address);
  await wide.waitForTimeout(950);
  assert.ok(await wide.locator('.hero').evaluate(hero => hero.getBoundingClientRect().height <= 750), 'hero is too tall on a wide desktop');
  assert.ok(await wide.locator('h1').evaluate(title => parseFloat(getComputedStyle(title).fontSize) <= 74), 'hero title is too large on a wide desktop');
  assert.ok(await wide.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'wide page overflows horizontally');
  await wide.screenshot({ path: '/tmp/capybara-landing-wide.png' });

  const mobile = await browser.newPage({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 1 });
  mobile.on('pageerror', error => errors.push(error.message));
  await mobile.goto(address);
  assert.ok(await mobile.getByRole('heading', { name: /Find the step that broke/ }).isVisible());
  assert.ok(await mobile.getByRole('button', { name: 'Play replay' }).isVisible());
  assert.ok(await mobile.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'mobile page overflows horizontally');
  await mobile.getByRole('group', { name: 'Connection methods' }).getByRole('button', { name: 'HTTP events' }).click();
  assert.ok(await mobile.getByText('python examples/local_events.py').isVisible());
  assert.ok(await mobile.locator('.showcase-stage').evaluate(stage => stage.scrollWidth > stage.clientWidth && stage.scrollHeight > stage.clientHeight), 'mobile interface preview must support panning');
  assert.deepEqual(errors, [], `browser errors: ${errors.join(', ')}`);
  for (const element of await mobile.locator('[data-reveal]').all()) await element.scrollIntoViewIfNeeded();
  await mobile.waitForTimeout(750);
  await mobile.screenshot({ path: '/tmp/capybara-landing-mobile.png', fullPage: true });

  console.log('Desktop, wide desktop, and mobile landing smoke test passed.');
} finally {
  await browser?.close();
  server.kill('SIGTERM');
}
