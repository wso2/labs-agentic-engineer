# @aep/web-search

Web search for a model connection whose provider runs no search tool of its
own. aep-api decides the strategy once, from the connection
(`capabilities.webSearch`, root
[ADR-0038](../../docs/decisions/ADR-0038-an-organization-has-one-model-connection.md)),
and each consumer asks this package for its implementation:

| Strategy | Implementation |
|---|---|
| `anthropic-server-tool` | none here: Anthropic runs it server-side |
| `ollama-api` | `searchOllama`: `POST /api/web_search` on the connection's own host |
| `none` | none: the consumer offers no search tool |

A new host with a search API is a new strategy in `modelconn.CapabilitiesOf`
and one implementation here.

## Consumers

- **The agents service** registers a platform-executed `web_search` tool over
  `searchOllama`, passing its host-guarded fetch.
- **Coding runs** get the same call as `aep-web`, a stdio MCP server
  (`src/aep-web.ts`, the `./aep-web` export) that both runtimes mount. The
  runner image builds it from source through the `web-search` named build
  context (`runners/AGENTS.md`).

```sh
aep-web <config.json>
```

The runner writes the config (base URL, key, the run's staged-secret values,
the denial sentence) to a 0600 file in a private directory. A file, not env or
argv: a runtime's MCP config can reach a command line, and an MCP child does
not reliably inherit its parent's environment.

## Rules

- **The key never follows the host.** The endpoint is derived from the
  connection's base URL, the key goes as `Authorization: Bearer` to that origin
  only, and a redirect is refused.
- **A query holding a staged secret is refused** before it leaves the process,
  in the runner's WebSearch rule's own words.
- **Results are capped**: at most 5 results, 4,000 characters of content each,
  400 characters of query. One measured page held about 170,000 characters.
- **Errors are written for the model**: they name the host and the status,
  never the key or the response body.
- **Dependency-free**, including the hand-written MCP transport, because the
  runner image installs this package from source with no lockfile of its own.

## Commands

`pnpm --filter @aep/web-search build | test | typecheck | lint`
