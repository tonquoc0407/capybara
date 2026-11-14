# Capybara landing page

This is the public landing page source. The trace replay in the browser uses a labelled synthetic run; it does not connect to a visitor's local Capybara instance or send trace data to a server. The TUI and local Web UI are part of the Capybara binary.

## Local preview

Requires Node 22 or newer.

```sh
cd site
npm ci
npm run dev
```

`npm run build` produces `site/dist`. The build copies six existing repo assets from `demo/` into the output. No API keys or build secrets are required. Fonts are bundled locally; the page makes no third-party runtime requests. The browser replay is synthetic; the public SDK case uses a pinned upstream bug reproducer with its trace and source linked from the page.

Run `npm run build && npm run test:smoke` to check the replay and mobile layout with Playwright. The browser binary is installed separately with `npx playwright install chromium`.

## Cloudflare deployment

The landing page is live at <https://getcapybara.tonquoc1412.workers.dev/>. It uses Workers Builds with the repository root as the build path:

| Setting | Value |
| --- | --- |
| Project name | `getcapybara` |
| Root directory | `/` |
| Build command | `cd site && npm ci && npm run build` |
| Deploy command | `npx wrangler deploy --assets ./site/dist --name getcapybara --compatibility-date 2026-11-14` |

The build needs the repository's `demo/` directory because it copies existing images and video. No API keys or build secrets are required.

### Cloudflare Pages alternative

Connect the `tonquoc0407/capybara` GitHub repository to Cloudflare Pages and use:

| Setting | Value |
| --- | --- |
| Project name | `getcapybara` if available |
| Production branch | `main` |
| Root directory | `site` |
| Framework preset | Vite |
| Build command | `npm run build` |
| Build output directory | `dist` |
| Node version | `.node-version` in `site/` sets 22 |

The Pages project name determines the `*.pages.dev` address. If you switch hosting, update the canonical and Open Graph URLs in `index.html`.

Before announcing the site, review the Homebrew command against the latest release and verify the links and replay on desktop and mobile. The deployed assets load over HTTPS.

The GradientText effect is sourced from [React Bits](https://reactbits.dev/) through the [reactbits.dev-skill](https://github.com/Philotheephilix/reactbits.dev-skill) registry helper. Its separate MIT + Commons Clause terms are preserved in [REACT_BITS_LICENSE.md](REACT_BITS_LICENSE.md); the project's root MIT license does not replace them.
