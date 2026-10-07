# `@wso2/prototype-cli` — design notes

`prototype init | check | preview | export`. Exit codes: 0 ok, 1 findings or a
failed operation, 2 a usage error (including an unknown theme or an explicit
busy `--port`).

## Preview

A local server bound to 127.0.0.1: the host page, `host.js` (prebuilt by esbuild
at package build time), the theme's `frame-runtime.js`, an SSE stream (`update`
with the last good revision, `findings`) and `POST /feedback`. It answers only
requests addressed to `127.0.0.1:<port>` or `localhost:<port>` and takes
feedback only as JSON from its own origin; a declared or streamed body over
1 MiB gets 413. The host page is sent with `frame-ancestors 'none'` and
`X-Frame-Options: DENY`. If a runtime file is missing the server answers 500
rather than crashing.

The watcher rechecks once the files are quiet for 120 ms and keeps the last good
revision, so a half-written file shows findings over the previous render. It
survives unreadable files: it shows a finding and keeps the last good render.
The kit's check runs off the event loop, so the server answers while a revision
renders; a newer change supersedes a check still in flight.

## Host page

Frames the app in the kit's `PrototypeWindow`; uses the kit's `/host` reducer for all view state, and its `/feedback` for the
request shape, limits, the Annotate queue helpers and the revision hash. `--persist` keeps snapshots
in `localStorage` under `proto:data:<revision hash>`; a new revision starts from
the seed. Annotate queues requests and saves `.prototype/feedback.json`. The
queue survives revisions and is saved with the hash it was started against
(`prototypeHash`); the panel notes "Queued against an earlier version" when the
current revision differs.

## Export

One HTML file: the host bundle, the frame runtime and the revision inlined as a
JSON config, escaped against `</script>` breakout (tested). Its CSP allows
inline script with `'unsafe-eval'` because the `srcdoc` frame inherits it;
`connect-src 'none'`. No Annotate, no persistence.

## Tests

Seam 1: the built bin (`test/*.test.ts`, node) and the browser lane
(`vitest.browser.config.ts`, `pnpm --filter @wso2/prototype-cli test:browser`):
tests run in the browser and drive Playwright pages through node-side commands
(`test/browser/commands.ts`). Seam 2: `test/consumer.test.ts` installs the packed
tarballs with npm in a temp directory.
