# @aep/console

The agentic-first console; see `AGENTS.md`. It replaced the earlier console,
kept as source at the `classic-console` git tag; `design/classic-console-gaps.md` lists what
that one has and this one does not yet.

## Run

```sh
pnpm --filter @aep/console gen   # API types + route tree
VITE_API_MODE=mock pnpm --filter @aep/console dev   # http://localhost:8090, no backend needed
```

Mock mode serves the API from MSW and signs in as a fixed dev user. Against a
real aep-api, drop `VITE_API_MODE`: the dev server proxies `/aep-api-service`
to `API_PROXY_TARGET` (default `http://localhost:9090`), and sign-in goes to
Thunder as `aep-console-client`. `VITE_AUTH_MODE=thunder` keeps MSW for the API
but signs in for real.

Against a `make dev-env` cluster:

```sh
API_PROXY_TARGET=http://console.ae.localhost:8080/aep-api-service \
COLLAB_PROXY_TARGET=ws://console.ae.localhost:8080 \
VITE_THUNDER_URL=http://thunder.openchoreo.localhost:8080 \
pnpm --filter @aep/console dev
```

The spec is the collab room: the app opens it at `/collab` on its own origin,
and the dev server forwards that to `COLLAB_PROXY_TARGET` (the in-cluster
console forwards its `/collab` to the collab server, so pointing at it works).

aectl registers only the in-cluster console's `/callback` on
`aep-console-client`, so sign-in from the dev server needs
`http://localhost:8090/callback` added to that client's redirect URIs in the
cluster's Thunder, by hand. A rerun of aectl's Thunder setup resets the list.
The in-cluster console (`http://console.ae.localhost:8080`) needs nothing.

Mock mode starts as an unconfigured org, so the onboarding wizard shows. Run
`localStorage.setItem("aep:mock:settings", "connected")` in devtools and reload
for a configured org; a connection saved in the wizard persists under
`aep:mock:connection:v2`, so remove that key to see the wizard again.

A build runs for about 25 seconds; Acme Expenses' first build fails one scenario (F2.4) so Fix has something to fix — `localStorage.setItem("aep:mock:build-result", "pass")` makes it pass.

Port 8090 is fixed (`strictPort`): the OIDC redirect URI names it, so the
server fails to start rather than move to another port.

## Test

```sh
pnpm --filter @aep/console test
```
