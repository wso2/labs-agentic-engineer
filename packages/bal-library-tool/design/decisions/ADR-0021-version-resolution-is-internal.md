# ADR-0021 — Version resolution is internal, and no document discloses it

Status: accepted · 2026-08-17

## Context

Three flags existed to pin a version: `--version` on every package verb, `--project-dir` to name a
component whose `Dependencies.toml` a build had written, and `overview`'s trailing positional. They were
correct and they were not used. Measured on the 2026-08-16 eval sweep:

- **`--version` was invisible where a caller would look.** The root `--help` listed it among six flags
  and said nothing about when it mattered; no agent in the sweep passed it once.
- **Its syntax differed per verb** — a positional on `overview`, a flag elsewhere — which is what
  ADR-0012 exists to prevent, appearing in the grammar rather than in the prose.
- **`package-not-found` said "verify the version is published"** while no verb could list what was
  published. That is advice naming a step the caller has no way to take.

So every lookup answered for Central's latest while the build compiled against whatever the project
locked, and nothing in either document said they might differ.

## Decision

**There is no version syntax in the grammar, and no document discloses the resolution.**

1. `LibraryTool` walks up from the process's directory for a `Ballerina.toml`. Found, the sibling
   `Dependencies.toml` supplies the version for the requested package.
2. Otherwise, Central's latest.

This is safe across package boundaries because `Dependencies.toml` carries the **transitive** closure:
measured, `maintenance_api` imports 8 packages directly and its lock file lists 36, `ballerina/auth`,
`crypto` and `jwt` among them. So ADR-0009's cross-package pointer drops its `--version` argument and
stays correct — the project already pins the far side of the edge. The version is still **printed** beside
each edge, because these signatures were generated against it and Central's latest may be a different
one; it is simply not an argument any more.

**Walking up happens in `LibraryTool` and nowhere else.** That class is the only one allowed to read the
environment, which is what keeps `Cli.run` drivable against a temporary tree and every test hermetic.

**T10 is closed in the failure rather than in the grammar.** The only reachable "the caller supplied a
version" path is now a `Dependencies.toml` that locks something Central does not publish — a real skew
between project and registry, where "omit the version" names a step there is no argument for. That
failure NAMES the published versions instead, which is why no `versions` verb is needed. The list is
fetched on a path that has already failed, so a second failure degrades the message rather than
replacing the failure the caller actually hit.

## Consequences

- A lookup and a `bal build` inside a project cannot disagree about which version they read, and nobody
  had to know to ask for that.
- **Known limit, accepted:** outside a project a lookup resolves Central's latest, which may differ from
  what a build would pick. For a package not yet in the dependency graph, latest IS the correct answer —
  it is what adding the import would resolve to.
- A version-shaped argument is rejected with the new rule stated, because there is no argument to move it
  to and reading it as a selector would report `symbol-not-found` on a "declaration" called `4.6.5`.
- `Loader.LoadOptions` lost its `version` field rather than keeping it as an unreachable escape hatch. A
  field nothing sets is a field nothing tests.
