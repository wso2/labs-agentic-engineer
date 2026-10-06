# @aep/sse-cassette

HTTP record/replay for streaming (SSE) endpoints. A cassette stores one real
HTTP exchange with the **exact response byte chunks, their boundaries, and
inter-chunk delays**, so tests can replay the agent's turn stream precisely as
production produced it — including the awkward parts (frames split across
chunks, multibyte characters split across chunks, real pacing).

## Record

Insert the proxy between the console and aep-api, then drive the UI normally
(the proxy is streaming-transparent):

```sh
pnpm --filter @aep/sse-cassette record -- \
  --target http://localhost:9090 --port 9091 \
  --out test/fixtures/turns --match '/turns$'
# then run the console with API_PROXY_TARGET=http://localhost:9091
```

`authorization`, `x-api-key`, `cookie`, … are redacted before a cassette
touches disk.

## Replay

In tests — the recorded body as a `ReadableStream<Uint8Array>`:

```ts
const cassette = loadCassette("fixtures/turns/001-post-….json");
cassetteToStream(cassette);                                  // recorded boundaries, no delays
cassetteToStream(cassette, { timeScale: 1 });                // recorded pacing
cassetteToStream(cassette, { rechunk: { seed: 7, maxBytes: 3 } }); // fuzz chunk boundaries
```

As a mock HTTP server (in-browser repro, Go tests, integration suites):

```sh
pnpm --filter @aep/sse-cassette serve -- --dir test/fixtures/turns --port 9092
# then run the console with API_PROXY_TARGET=http://localhost:9092
```

Requests match on method + path (volatile id segments wildcarded) + the JSON
body's `useCase`; duplicate recordings of the same key are served in recorded
order. `--fallback http://localhost:9090` proxies unmatched requests to the
live `aep-api` so a real browser session works end to end.

## Inspect

Dump a cassette's ordered SSE event sequence — one line per frame, with the
network-chunk range it arrived over and the arrival time of the chunk that
completed it (chunks and frames are different sequences: one chunk can carry
several frames, one frame can span many chunks):

```sh
pnpm --filter @aep/sse-cassette exec tsx src/cli.ts events \
  --file test/fixtures/turns/006-….json.gz
#  idx    arrival  chunks    event
#    5     1611ms  chunk 4         tool-input-start  tool=loadSkill id=toolu_01RY…
#  143    61486ms  chunk 130–131   tool-call  tool=addFile id=toolu_01Aw…
```

`--type tool-call` filters to one part type; `--json` emits the full ordered
array (`index`, `kind`, `type`, `part`, `raw`, `chunkStart`, `chunkEnd`, `tMs`)
for programmatic digging. The same data is available in tests via
`cassetteEvents(cassette)`.
