# ADR-0003 — A raw-payload cache keyed by coordinates, and silent about itself

**Status:** Accepted · shipped 2026-08-10

## Context

Each invocation of the reader costs **4.9 to 6.6 seconds**, dominated by a 12.4MB
JSON download for `ballerinax/github`. That is *why* the command could only be
asked once per package, and therefore why it emitted 21,818 lines to be navigated
by hand.

**The cache is not a speed optimisation; it is what makes an addressed lookup
possible at all.** Once re-opening a package costs about 200ms, the CLI can be
asked four precise questions instead of one broad one. Shipping
[ADR-0002](ADR-0002-four-addressed-documents-in-two-registers.md)'s verbs without
it would leave the 12-turn floor exactly where it is; shipping the cache without
the verbs would too.

Measured, live, against Central:

| | |
|---|---|
| cold `bal-library ballerinax/github` | **5.06s** |
| warm, same command | **0.21s** |
| on disk | 12,397,105 bytes |
| every verb on one warm payload | one fetch, four documents |

## Decision

### The raw payload, not the IR

Cached inside `fetchDocs` — **above** the retry loop, so a hit costs no attempt,
and **below** the schema, so what is stored is not derived from our own code.

Raw, because the IR and the rendering *are* our code and ADR-0002 changes three of
those modules. An IR entry would need a build identity in its key, and a runner
whose baked bundle differs from a mounted `dist/` would serve output from the wrong
renderer. The raw payload is not derived from our code, so the coordinates are the
whole key. Re-deriving costs about 110ms to parse, validate and transform
`ballerinax/github`, against 5 to 7 seconds to download it.

### Uncompressed

Disk is not the constrained resource: the runner pod declares no
ephemeral-storage limit, both its mounts are emptyDirs, and the cache does not
outlive the run. Compression would add a level to choose, a corruption mode to
handle, and a compress step on the write path, to save bytes nobody is paying for.

### Layout and location

```
<root>/v1/docs/<org>/<name>/<version>.json      mode 0600, no TTL
<root>/v1/latest/<org>/<name>.json              {"version":"6.0.0","atMs":…}
```

`v1` is the on-disk format generation, bumped only when the stored bytes change
meaning. Deliberately **not** a build identity.

The root is a pure function of `{env, homedir, tmpdir, uid}`:

1. `BAL_LIBRARY_CACHE=off` → no cache
2. `BAL_LIBRARY_CACHE_DIR=<dir>`
3. `$XDG_CACHE_HOME/bal-library`, when absolute
4. `<homedir>/.cache/bal-library` — **the default**
5. `<tmpdir>/bal-library-<uid>`, mode 0700, when `$HOME` is unusable
6. otherwise no cache

`~/.cache` because it is conventional; because `$HOME` in the runner is owned by
the run user with nothing mounted over it but the workspace; because it is not
world-writable, so the `/tmp` symlink-precreation hazard does not arise; and
because in playground `--host` mode it lands in the developer's real `~/.cache`,
the only surface with cross-run warmth. Not beside the bundle, which the playground
mounts read-only. Not in the workspace, which is a git clone the platform commits
and provisioning scrubs per task.

### Path safety, in three independent checks

Every coordinate is validated raw; every path segment is validated again after a
suffix is attached; and the resolved path must still start with the root plus a
separator. `parseQualifiedName` and `parseVersion` reject `.` and `..` before any
of that, which makes these the inner guards rather than the only ones.

The raw check is not redundant with the segment check: `..` with `.json` attached
becomes `...json`, which is an ordinary filename and passes. That traverses
nothing, but a coordinate this store would refuse as a directory name should not be
accepted as a file name either, or the two guards disagree about what a valid key
is.

### Every way an entry can be wrong is a miss

Read, `JSON.parse`, **coordinate-check the raw object**, then `parseCentralDocs`.
A miss is: absent, unreadable, truncated, not JSON, JSON but not a payload, schema
drift, or coordinates that do not match the entry's own path. The entry is
best-effort unlinked and the network is used, so a corrupt entry cannot produce a
wrong document and heals on the next successful fetch.

The coordinate check runs on the **raw** JSON because zod strips exactly the two
fields it needs: `moduleSchema` has no `version` and `centralDocsSchema` has no
`apiDocsVersion`. Both are on the wire, verified present in all nine fixtures.
Adding them to the schema instead would make them required reads and turn a
cosmetic upstream change into a failed lookup, which is the trade `schema.ts`
deliberately does not make.

Module matching uses the **requested** name, which is why `selectModule` had to
stop reading `modules[0]` first: a check that verifies one module while the
renderer reads another verifies nothing.

A payload the schema rejects is **never written**, so a drift is not made permanent
for every later run.

### Cache trouble is never the caller's problem

`mkdir` failure, ENOSPC, a foreign uid, a root that `lstat` shows is a symlink, an
unusable `BAL_LIBRARY_CACHE_DIR`: all of them disable caching **silently** — no
byte on stdout, no byte on stderr, no non-zero exit, no `cache` failure kind and no
third exit code.

Making an unusable directory exit 2 would send the agent into the skill's
argument-error advice in a loop it can never escape. The one place the cache speaks
is `--help`, which already sits outside both the document and the `Failure`
contract: it prints the resolved directory and whether it is writable, which is how
an operator proves the cache is alive inside a runner image.

### No locks

Concurrent writers are structural, not hypothetical: fan-out is the runner's
default, subagents are foreground, and recorded runs show two subagents' bash calls
interleaving seconds apart in one container sharing one `$HOME`.

Write to `<final>.<pid>-<rand>.tmp` in the same directory at mode 0600, then
`rename()`, which is atomic on POSIX. Two processes that miss the same package both
fetch and both rename, the content is equivalent, and no third process can observe
a partial file. A lock could outlive the client's own 300s budget and hang a run; a
duplicate 5.7s download is the cheaper failure.

### TTL, refresh, offline

Docs entries are immutable coordinates and **never expire**. The versions list is
the one mutable response and gets a **10-minute TTL**: the measured lookup episode
runs 70 to 260 seconds, so one TTL spans a whole episode without a second registry
round trip (1.0 to 1.5s each), while a publish is still picked up inside a single
run. A future-stamped entry is treated as wrong rather than fresh, so a clock that
jumped backwards cannot make one immortal.

`--refresh` **unconditionally** unlinks the docs entry and re-resolves the version.
An earlier draft made the re-download conditional on the version having changed,
which made it a no-op in exactly the case its own error message recommends it for.

**Offline.** When the registry fails with `upstream` or `timeout`, fall back to an
expired `latest` entry, then to the highest version on disk, stamping
`cache (stale: registry unreachable, version unverified)`. "Highest" needs a real
comparator, not lexicographic: otherwise `1.9.0` sorts above `1.10.0` and
`2.0.0-alpha` above `2.0.0`. Only when the disk has nothing does the failure
propagate. Without this, a warm payload plus one registry blip is a hard failure
that can burn the full 300s budget — four times over in a four-verb episode.

## Rejected

| option | why not |
|---|---|
| Caching the IR or the rendered string | Cheaper to read, but it bakes in three modules ADR-0002 changes, so its key would need a build identity and a rebuilt bundle would have to invalidate it. |
| Caching at `fetchJson`, keyed by URL | Cannot tell the immutable docs endpoint from the mutable versions list. |
| A lock file or single-flight fetch | A lock can outlive the client's own 300s budget and hang a run. A duplicate download with an identical write is cheaper. |
| Compression | See above: no level to choose, no corruption mode to handle, and nobody is paying for the bytes. |
| A cache marker in a document body, or `--no-cache` | Provenance lives on the header only. Bypass is `BAL_LIBRARY_CACHE=off`; refresh is `--refresh`. |
| A `cache` failure kind or a third exit code | Cache trouble is never the caller's problem. |
| `/tmp` as the default root | World-writable and shared with the agent's own `/tmp/*-api.bal`. Kept only as the `$HOME`-unusable fallback, mode 0700, uid in the name. |
| A PVC, a hostPath, or a pre-warmed cache in the runner image | The Job template hard-codes two emptyDirs; the image is already multi-GB with a 28-minute cold build under a 45-minute CI ceiling. |
| A resident daemon holding the IR in memory | Needs a socket, a lifecycle nobody owns, and concurrency handling for interleaved subagents, against a delivery constraint that the whole thing stay one dependency-free `.mjs` plus a two-line launcher. |
| An eviction policy | In production the cache dies with the run. In host mode `rm -rf ~/.cache/bal-library` is the answer. |
| A cache library (`cacache`, `flat-cache`) | cacache is content-addressed with an index and integrity hashes — machinery for a problem this does not have; flat-cache holds everything in memory, and github alone is 12.4MB. Either would still need the never-throw wrapping, which is most of what `disk.ts` is. |

## Consequences

- **In production this is a within-run cache.** Both runner mounts are emptyDirs
  and the playground container is `--rm`. Stated plainly against the original
  request — "cached in the first run and then it uses that cached response" holds
  *within* a run, which is where all four invocations of an episode happen, and it
  is what makes the addressed verbs affordable. It does not carry a warm cache from
  one task's pod into the next.
- **A github entry is 12.4MB on disk.** Free in production; a few tens of megabytes
  in a developer's `~/.cache` in playground host mode.
- **The provenance header makes stdout run-order-dependent.** The same command
  prints `central` then `cache`. This is a real if small change to the
  stdout-is-the-document discipline, and it is what buys an operator the ability to
  tell a hit from a fetch and an agent the warning that a version was never
  verified.
- **One invariant to keep:** the cache key has **no identity dimension**. That is
  correct only while `fetchJson` sends no headers and only public Central data is
  reachable. If a Central token is ever threaded through `HttpOptions`, this cache
  must be disabled or keyed by a token fingerprint — `$HOME` outlives the per-task
  workspace scrub, and mode 0600 buys nothing against the same uid.
- **`src/` still reads zero environment variables.** Only `main.ts` resolves the
  location and constructs the store, which is what keeps the whole test suite
  hermetic: tests drive `run()` with an injected cache and cannot touch a
  developer's real `~/.cache`.
