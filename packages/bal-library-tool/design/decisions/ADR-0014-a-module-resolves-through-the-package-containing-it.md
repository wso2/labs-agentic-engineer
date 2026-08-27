# ADR-0014 — A module resolves through the package containing it

**Status:** Accepted — closes the half of S3-01 that ADR-0009 left open

## Context

`registry/packages/<org>/<name>` lists **packages**. A module of a package has no row of its own, so
resolving a version for one returned 404 and the reader could not read it at all:

```
$ bal library overview ballerinax/aws.auth
{"kind":"package-not-found", "suggestion":"'aws.auth' may be a MODULE of a package rather than a
 package … Pin the version with --version <version> to read it directly; the version is printed
 beside the name in the `--deps` footer that referred you here."}
```

Nothing about the document was missing. `docs/<org>/<module>/<version>` answers for a module perfectly
well, at the version of the package that contains it — measured directly:

| request | status |
|---|---|
| `registry/packages/ballerinax/aws.auth` | 404 |
| `registry/packages/ballerinax/aws` | 200 → `["1.0.1", …]` |
| `docs/ballerinax/aws.auth/1.0.1` | **200** |
| `docs/ballerinax/aws.auth/9.9.9` | 404 |

So the version was one question the registry *could* answer — asked about the parent — and the reader
was delegating it to the caller instead.

ADR-0009 made the `--deps` footer print the pinned command, which made the module reachable **once the
caller had run `--deps`**. The eval sweep of 2026-08-15 measured what that leaves behind. The inline
`// Special Agent Note: AuthConfig FROM ballerinax/aws.auth module` prints unconditionally and names a
coordinate; a caller that has not passed `--deps` has no footer, no version, and every reason to use
the coordinate as printed. Two of seven cases did exactly that and spent **seven lookups** and both of
the sweep's dead-end exits getting out — one of them by resolving `ballerinax/aws` by hand purely to
harvest a version string. The suggestion compounded it by pointing at "the `--deps` footer that
referred you here", which had referred nobody.

## Decision

**`resolveLatestVersion` resolves a module through the package containing it.** The full name is tried
first, then — only on `PackageNotFound` — each shorter dot-prefix in turn.

- **Full name first, always.** A dotted name is far more often a package than a module:
  `ballerinax/googleapis.sheets`, `ballerinax/aws.s3` and `ballerinax/googleapis.gmail` are all
  packages. Falling back on a *hit* would put a second round trip on every one of them.
- **Each shorter prefix, longest first.** `a.b.c` is a module of `a.b` when that exists and of `a`
  when it does not; both are legal, so the walk cannot stop at the first prefix. An undotted name has
  no prefixes and cannot reach the fallback at all.
- **A wrong guess cannot survive.** The version is only ever used to fetch `docs/<org>/<module>/<v>`,
  and a module that was not published at that version is a 404 there.
- **The module's own key is written to the version cache**, so a second lookup costs one call instead
  of re-walking. A module carries its package's version, which is the same fact that makes reading it
  at that version correct.

**A 404 from the docs endpoint blames the half the caller can act on.** The old text — *"Verify version
'X' is published; omit the version to take the latest"* — is correct only when the caller chose the
version. After this change a resolved version reaches the docs endpoint on the caller's behalf, and
that advice would name a command they never wrote and a step they already took. `ResolvedVersion`
carries `supplied` (a `--version` flag, or a `Dependencies.toml` the reader was pointed at), and the
message follows it: a supplied version is theirs to correct, a resolved one means the **name** is
wrong.

**The dotted branch of `notFound` is gone.** Advice about pinning a version now describes a recovery
the reader has already attempted, which reads as an untried option. When the walk finds nothing, the
failure names what it tried instead:

```
Central publishes no package under this name, and none of the packages it could be a module of
exists either (tried ballerinax/aws). Check the org/name spelling; `bal library find aws.auth`
lists what Central publishes.
```

*(The verb was `search` when this was accepted; ADR-0019 renamed it to `find`. The text above is what
the tool prints today — this failure names a command, so it had to move with the rename rather than
stay a historical quotation.)*

## Consequences

- `bal library type ballerinax/aws.auth AuthConfig` works with no flags, returning the seven-member
  union ADR-0009 recorded as "unreachable". The follow-up an agent reaches from the inline note is
  now the one it was already going to type: six calls collapse to one.
- **The `--deps` footer keeps printing `--version`, for the reason that survived.** Its own sentence
  used to give two: version agreement, and making a module readable at all. Only the first is still
  true — sap's signatures are documented against `ballerina/http` 2.15.4 while Central's latest is
  2.16.6 — so the printed line now says that and only that.
- `type --help`'s example changed from `ballerinax/aws.auth AuthConfig --version 1.0.1` to the same
  command without the flag, plus `ballerina/http Response --version 2.15.4` for the agreement case.
  ADR-0011 makes `--help` the whole agent contract; leaving the old example would have taught the
  belief this change exists to remove.
- **Cost is bounded and paid only by names that need it.** An undotted name is unchanged. A dotted
  name that is its own package costs one call, as before. A module costs one extra registry call the
  first time and none afterwards.
- The walk cannot mask an outage: only `PackageNotFound` falls back, so a 500 or a timeout on the
  module's own row is still reported as what it is rather than being converted into "no such package".
