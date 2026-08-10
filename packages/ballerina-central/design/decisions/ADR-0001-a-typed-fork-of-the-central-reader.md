# ADR-0001 — A typed fork of the Central reader, delivered as a command

**Status:** Accepted · shipped 2026-08-09 · replaces the vendored
`skills/ballerina/scripts/` tree and the `VENDORED.md` that documented the
opposite decision.

## Context

[`ballerina-platform/skills`](https://github.com/ballerina-platform/skills)
ships a Ballerina Central reader as an MCP tool, `get_library`: it fetches a
package's docs from `api.central.ballerina.io` and renders the whole API as a
compact Ballerina syntax string. Upstream hides it behind a `library` subagent.

The reader itself has no third-party runtime dependencies — `node:fs`,
`node:path`, global `fetch` — so the capability is reachable without MCP at all.
A spike vendored `mcp/src/*.js` verbatim into `skills/ballerina/scripts/` with a
small CLI front end, and an A/B against a one-component Ballerina service
measured it worth having (numbers in [`../../README.md`](../../README.md)).

The spike also surfaced two defects, both of which say something about where the
code should live:

1. **CommonJS in an ESM project.** Node picks a `.js` file's module system from
   the nearest `package.json`. The skill library gets mirrored into a project
   workspace, and if that project declares `"type": "module"` every `require`
   becomes a `ReferenceError`. One measured run died exactly this way, fell back
   to the `.bala` tree, and came out *worse* than the no-reader baseline.
2. **Latest-version resolution cost 45 seconds.** Upstream resolves "latest" by
   listing the org's entire registry and filtering client-side; `ballerinax` has
   about a thousand packages.

Neither is a bug in the reader's rendering. Both are consequences of shipping
executable code inside a prose artifact that gets copied into places nobody
controls.

## Decision

### 1. A real package, not a vendored script tree

`packages/ballerina-central`, ported to TypeScript under the repo's existing
strict compiler settings, with a fixture corpus and snapshot tests.

The cost is stated plainly: **this forks upstream permanently.** `cp -R` is no
longer a refresh path and upstream fixes stop arriving for free. What buys it is
that the failure mode we actually fear — Central changes a field, the reader
renders a subtly wrong signature, and nobody learns until an agent writes
uncompilable Ballerina — is invisible in the vendored version and loud here.
518 lines of hand-walking an untyped payload became a zod schema at the
boundary; a renamed or removed field is now a located `schema-drift` failure.

### 2. A command on `PATH`, not a file in the skill directory

The skill says `bal-library <org/name>` and nothing about paths.
`skills/playwright-cli/` was already this shape, and it is the right one:

- nothing but prose reaches an org's skills repo, so there is no build output
  for an org to edit or delete out from under a `SKILL.md` that points at it;
- no committed bundle and no freshness gate in CI;
- **the CJS/ESM trap is unrepresentable** — one bundled `.mjs`, whose extension
  fixes its module system wherever the file lands, plus a two-line `sh`
  launcher, which has no module system to get wrong.

Delivery is a directory on `PATH` rather than a symlink into `/usr/local/bin`,
because that is what lets the playground bind-mount its working-tree build over
the baked copy and keep skill and command on one iteration loop. A symlink would
resolve through its own path and defeat the launcher's `dirname "$0"`.

### 3. Failures are values, and there are two exit codes

A discriminated `Failure` union with `Result<T>`, mapped to exit codes by a
total function. `2` means the caller's own arguments are wrong and re-running
unchanged cannot help; `1` means the lookup itself failed and running again
could legitimately give a different answer. The distinction matters because the
two ask the reader for different next actions — and since §5 removed the
fallback source, neither is ever an invitation to guess a signature, which is
why each failure carries its own `suggestion`.

### 4. Required fields are strict; unknown keys are not

Deliberately split, and a deviation from the design draft's "`.strict()`
everywhere":

- fields the reader reads are **required**, so a rename, a removal or a type
  change fails loudly — those are the changes that would make us render the
  wrong thing;
- unknown keys are **stripped**, because Central adding a field is harmless to a
  reader that does not read it, and failing the command over one would take the
  capability away for a cosmetic upstream change;
- additions are still caught, by `test/keyspace.test.ts` snapshotting the
  payload's whole key space — a reviewable diff, at no run-time cost.

`module.description` is the single exception, read but optional (§5).

### 5. The guide is the second document — and it retires the `.bala` fallback

The skill used to end with "when the command does not answer, read the on-disk
`.bala` tree", and that fallback existed for two things. Its second half —
`modules/` holds the exact signatures — the API document had already replaced.
Its first half had not: **`docs/README.md`**, the package's own guide, which
leads with runnable samples. That file turns out to be `module.description` in
the payload every lookup already fetches — byte-identical for
`ballerinax/kafka@4.6.5`, 7,463 bytes, zero diff — and the schema was throwing
it away.

So `--readme` prints it, and **the skill no longer mentions the `.bala` tree at
all.** The command is installed on the runner image and mounted in both
playground modes; a skill carrying a second, differently-shaped source for the
same two documents costs every reader the choice between them, permanently, to
insure against a delivery failure that would be a bug to fix rather than a path
to route around. Four consequences:

- the guide answers **before any build has resolved the package**, which the
  tree could not — and that is exactly when a connector nobody has written
  against is hardest to guess at;
- it costs no extra request, because the guide rides the docs response;
- `description` is read but **optional**, deviating from §4. It is the one field
  whose absence must not cost the caller anything else: a package that never
  wrote a `Module.md` should still render its API. `--readme` reports the
  absence itself, as a `no-readme` failure at exit 1;
- with nowhere to fall through to, a non-zero exit has to be actionable on its
  own, so every `Failure` carries a `suggestion` and the skill says plainly that
  a failed lookup is never grounds for improvising a signature.

Markdown on stdout rather than Ballerina is a widening of the stream contract,
so the version stamp widened with it: `<!-- Resolved: … -->`, an HTML comment,
which greps like the `// Resolved:` line and renders as nothing.

### 6. Per-package version resolution

`registry/packages/<org>/<name>` returns one package's versions, newest first,
in about a second, against ~45s for upstream's org listing. Kept from the spike
and measured there.

## The one output change

The port is byte-identical to the vendored JS across the whole corpus except 13
lines in 2 of 9 fixtures, all the same defect:

```diff
-    remote function close() returns sql:Error?;
+    function close() returns sql:Error?;
```

Upstream decides "is this a remote function" by asking whether the IR object has
an `accessor` key, so every non-remote method on a client class — `close()` on
`postgresql:Client`, `getCookieStore()` and the circuit-breaker calls on
`http:Client`, `getRemoteHostName()` on `http:Caller` — is declared `remote`.
An agent following that writes `dbClient->close()`, which does not compile;
Ballerina requires `dbClient.close()` for a plain method. `http:Caller`'s own doc
comment in the same output shows the dot call.

This is exactly the class of defect the exercise exists to remove, and `close()`
on a database client is not an edge case, so it is fixed rather than preserved.
It is recorded here because it is the *only* behavioural difference: the
equivalence oracle existed precisely so a port bug and a deliberate improvement
could not be confused, and it did its job.

## Rejected

| option | why not |
|---|---|
| Register upstream's MCP server on the runner | An install channel, a lifecycle and a per-turn failure mode for one tool. `resolveBaseAgentConfig` fixes a session's tool surface deliberately. |
| Add the package as a workspace dep of remote-worker | `runners/remote-worker/package.json` has zero workspace deps by design — standalone `npm ci` from its own lockfile. |
| Ship TS source and invoke `npx tsx` | tsx is not guaranteed where the skill runs, costs startup per call, and puts a TS toolchain in every org skills repo. |
| Keep the vendored JS | No types, no tests, no coverage across libraries — and it renders `remote function close()`. |
| Constrain `accessor` to the HTTP methods | A Ballerina resource accessor is an identifier; `subscribe` is as legal as `get`. The guarantee worth having is structural (accessor and path are separate fields), and that is kept. |

## Consequences

- Refreshing from upstream is now a read-and-port exercise, not a copy. The
  corpus is what makes that safe: re-render, review the snapshot diff.
- Every runner-image build path must pass the `balcli` named build context —
  `build-runner.sh`, `release.yml`'s matrix row, `local/run-local.sh` — and the
  release workflow gained a build step, because the bundle is a build output
  rather than a checked-in file.
- Output size is unchanged: `ballerinax/github` still renders to 21,818 lines.
  The win is navigation, and it depends on the caller grepping rather than
  paging, which is why the skill says so twice. Reordering the output so clients
  precede types would fix that at the source and is the obvious next change;
  it moves every snapshot, so it wants its own reviewed diff.
