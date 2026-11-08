# Capybara landing page

This is the public landing page source. The trace replay in the browser uses a labelled synthetic run; it does not connect to a visitor's local Capybara instance or send trace data to a server. The TUI and local Web UI are part of the Capybara binary.

## Local preview

Requires Node 22 or newer.

```sh
cd site
npm ci
npm run dev
```

`npm run build` produces `site/dist`. The build copies four existing repo assets from `demo/` into the output. No API keys or build secrets are required. Fonts are bundled locally; the page makes no third-party runtime requests.

Run `npm run build && npm run test:smoke` to check the replay and mobile layout with Playwright. The browser binary is installed separately with `npx playwright install chromium`.

## Cloudflare Pages setup

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

The project name determines the `*.pages.dev` address. Check name availability in Cloudflare before putting the URL into documentation. Do not set a canonical URL or sitemap until the deployed address is confirmed. The repository's `demo/` directory must remain available to the build because the site reuses its images and video.

Before making the site public, review the Homebrew command against the latest release, verify the links and the replay on desktop and mobile, and confirm the deployed page loads its assets over HTTPS.

The spotlight effect is sourced from [React Bits](https://reactbits.dev/) through the [reactbits.dev-skill](https://github.com/Philotheephilix/reactbits.dev-skill) registry helper. Its separate MIT + Commons Clause terms are preserved in [REACT_BITS_LICENSE.md](REACT_BITS_LICENSE.md); the project's root MIT license does not replace them.
