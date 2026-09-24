# Browser regressions

These tests use synthetic data only. They build a temporary Salt binary, start it
on a randomly selected loopback port with a fresh temporary data directory, and
remove it afterwards. They never connect to an existing Salt instance.

From the repository root, with Go and Node.js installed:

```sh
npm ci --prefix web
npm run build --prefix web
npm ci --prefix web/tests
npx --prefix web/tests playwright install chromium
npm test --prefix web/tests
```

`GO_BINARY` may select a Go executable. To use an already installed Chrome instead
of downloading Chromium, skip the browser installation and set
`PLAYWRIGHT_CHANNEL=chrome` when running the tests.

The browser dependency is isolated in this test package; the application has no
additional runtime dependencies. Tests do not upload fixtures or screenshots.

Callout coverage: all seven preset icons, immediate background changes when
cycling icons, existing saved blocks, light/dark appearance, dark-mode text
contrast, and persistence after reload without changing the callout text.
