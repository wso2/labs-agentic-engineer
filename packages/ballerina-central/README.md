# @aep/ballerina-central

Reads a Ballerina package's public API off [Ballerina
Central](https://central.ballerina.io) and answers four different questions about
it — addressed by name and by path, not grepped.

Ships as the `bal-library` command the `ballerina` skill drives.

```bash
bal-library ballerinax/github                              # the overview: guide + signatures
bal-library ops  ballerinax/github repos                   # navigate 903 operations
bal-library type ballerinax/github FullRepository          # one declaration, whole
bal-library type ballerina/http ClientRequestError --deps  # an error and its chain
```

## Why it exists

A coding agent writing Ballerina needs one thing the toolchain will not give it
until too late: the exact signature of a connector call. Before the first
successful `bal build` there is no `.bala` tree to read, and after one there is
a 900KB `types.bal` to page through.

Measured on a one-component service against `ballerinax/github`, two runs per
arm with the connector pre-pulled: with this capability, **51 and 51 turns,
$1.47 and $1.35**; without it, **59 and 68 turns, $1.61 and $1.99** — roughly a
fifth fewer turns, a fifth cheaper, and far tighter variance. Both arms produced
correct, clean-compiling code; the difference is that one arm reached the
signature by grep and the other spent six calls navigating to the `.bala` tree
and then read a 902KB `types.bal` three times. Wall clock was serving-latency
noise at that sample size and no claim is made about it.

That measurement was taken when the command printed one document to be grepped.
The addressed verbs are aimed at the remainder: on the recorded golden run, one
feature — read a repository's star count and handle the HTTP error — cost **12
bash turns and about 27,000 characters**, including two turns that returned 31
and 13 characters of nothing while the agent probed for where a record ended.
The same feature is now **four turns and about 20KB**, and the fourth no longer
depends on the model having GitHub's REST API memorised.

## Contract

| stream / code | content |
|---|---|
| stdout | the requested document, and nothing else, and only on exit 0 |
| stderr | on failure, one JSON object matching `Failure`; or usage text |
| exit 0 | success, and stdout is complete |
| exit 1 | Central could not answer |
| exit 2 | arguments wrong, `--help`, unknown verb, ambiguous client, or a `type` name that does not resolve |

```
bal-library <org/name> [version]                        the overview (default verb)
bal-library overview <org/name> [version] [--client C]  same, explicit
bal-library ops  <org/name> [path] [--client C] [--sigs]
bal-library type <org/name> <Name>... [--deps]
bal-library api  <org/name>

  --project-dir <dir> · --refresh · -h        every verb
  --client <Name>                             overview, ops
  --sigs                                      ops
  --deps                                      type
  BAL_LIBRARY_CACHE=off · BAL_LIBRARY_CACHE_DIR=<dir>
```

**A flag a verb does not take is exit 2, not a silently dropped argument.**
`overview <pkg> --deps` names the verb that would have taken it. Silently ignoring
it is the same class of mistake as an unknown flag resolving to a version: the
caller believes it asked for something it did not get, and nothing in the output
says otherwise.

A first positional containing `/` is a package; otherwise it is a verb. Verbs
**lead** for a reason: a verb has no slash, so a stale binary fails it against
the qualified-name regex at exit 2, while a verb placed *after* the package would
land in the version slot and come back as `package-not-found` at exit 1 — which
the skill teaches means "retry".

Version resolution: an explicit argument, then `--project-dir`'s
`Dependencies.toml` if a build has written one, then Central's per-package
versions endpoint. Every verb shares it, because a document describing a
different version than the one the component compiles against is worse than none.

## Four documents, two registers

A document either **is** Ballerina or it **describes** a package, and the two are
never blended. Mixing them produces things that look like declarations, are not,
and invite a reader to transcribe from them.

| | answers | register |
|---|---|---|
| `overview` | *how is this used, and what is callable?* | Markdown; signatures only inside fenced blocks |
| `ops` | *what operations exist, and under which path?* | Markdown |
| `ops --sigs` | *what is the exact signature of each of them?* | Markdown, signatures fenced |
| `type` | *what exactly is this declaration?* | raw Ballerina |
| `api` | *everything, when nothing above answered* | raw Ballerina |

`overview` carries the package's guide, every client's constructor and function
signatures, the module-level functions, and the **error declarations** — and no
other types, because they are 738KB of `ballerinax/github`'s 927KB. Errors stay
because they are the only declarations unreachable from a signature: github
declares zero and all 903 of its operations return the language-level `error`, so
nothing in the API document names `http:ClientRequestError`.

The guide is `module.description` from the docs payload, byte-identical to the
`docs/README.md` a published `.bala` carries — verified against
`ballerinax/kafka@4.6.5`. Reading it here rather than off disk is what makes it
available *before* a build has resolved the package. Its headings are demoted two
levels so `grep '^## '` returns the overview's own sections rather than the
readme's.

A client with more than 100 resource functions, or more than 20KB of signatures,
is replaced by its **path tree**, which `ops` navigates. `ballerinax/github`'s 903
operations are 36 top-level segments in 445 bytes, and each level names the next
command. Path matching is anchored segment-wise from the first segment: a suffix
match for `repos/{owner}/{repo}` returns nine operations rather than three,
because two unrelated team-access subtrees end in the same segments.

`type` takes several names and is **all-or-nothing** — if any one fails to
resolve, stdout gets nothing and one JSON object lists every unresolved name with
candidates. That is what keeps "exit 0 means stdout is complete" true for callers
that redirect. `--deps` appends the transitive **same-package** closure and names
cross-package edges in a footer with the exact follow-up command, rather than
hiding a five-second cold fetch inside an answer the caller expects to be warm.

`public` is stripped from every declaration. The document is a compact reading
aid rather than compilable source, and `public` is noise on 1,101 records — but it
is a deliberate decision rather than an accident, and `type` is why grepping for a
declaration header is no longer the way to find one.

## The cache

The raw Central payload, uncompressed, keyed by `org/name/version` and stored at
`~/.cache/bal-library` by default.

It is not a speed optimisation. At 4.9 to 6.6 seconds and 12.4MB per invocation
the CLI could only be asked **once** per package, which is what forced it to emit
21,818 lines to be navigated by hand. Measured: 5.06s cold, **0.21s warm**, which
is what makes four addressed questions cheaper than one big answer.

| | |
|---|---|
| what | the payload as Central served it — not the IR, whose key would need a build identity |
| where | `BAL_LIBRARY_CACHE_DIR`, then `$XDG_CACHE_HOME/bal-library`, then `~/.cache/bal-library`, falling through to `<tmp>/bal-library-<uid>` if that is not usable |
| TTL | docs entries never expire (immutable coordinates); the versions list gets 10 minutes |
| writes | temp file plus `rename()`, no locks — two runs that miss both fetch and both rename |
| offline | registry unreachable plus a payload on disk answers anyway, stamped `… version unverified` |

The location resolver returns the candidates in order and `main.ts` takes the
first that works, because "unusable" is not something a pure function can see: an
empty or relative `$HOME` it can, a `$HOME` that exists and is read-only — a shape
a container genuinely has — it cannot. An **explicit** `BAL_LIBRARY_CACHE_DIR` gets
no fallback, because caching somewhere other than the directory somebody named
would be worse than not caching.

**Every failure is silent.** An unusable directory, a foreign uid, a full disk, a
truncated entry, an entry whose coordinates do not match its own path: all of them
mean "no cached copy", with no byte on stderr and no non-zero exit. Making an
unusable `BAL_LIBRARY_CACHE_DIR` exit 2 would send the agent into the skill's
argument-error advice in a loop it can never escape. The one place the cache
speaks is `--help`, which prints the resolved directory and whether it is
writable.

**In production this is a within-run cache.** Both runner mounts are emptyDirs
and the playground container is `--rm`, so it does not survive the run — which is
where all four invocations of a lookup episode happen anyway. Cross-run warmth
exists only in playground `--host` mode; `rm -rf ~/.cache/bal-library` is the
answer there.

One invariant: **the cache key has no identity dimension**, which is correct only
while `fetchJson` sends no headers and only public Central data is reachable. If a
Central token is ever threaded through `HttpOptions`, this cache must be disabled
or keyed by a token fingerprint — `$HOME` outlives the per-task workspace scrub,
and mode 0600 buys nothing against the same uid.

## Delivery

The bundle is one dependency-free `.mjs` plus a two-line `sh` launcher, so
installing it is a copy and a `PATH` entry:

| where | how |
|---|---|
| runner image | `--build-context balcli=…/dist` → `/opt/ballerina-central`, on `PATH` |
| playground, docker mode | the same `dist/` bind-mounted over it — edit, rebuild the package, run |
| playground, host mode | `dist/` prepended to the coding child's `PATH` |

```bash
pnpm --filter @aep/ballerina-central build   # ~2s; no image rebuild in either mode
```

## Tests

284 tests in about 3.5 seconds, so the whole thing is a per-PR gate.

`test/views-agree.test.ts` is the one that matters, and a reviewer should refuse
the addressed verbs without it. The risk the verbs introduce is not a document
that looks wrong — it is one that shows a signature the package does not have
while `api` shows the right one, with nothing in either to say they disagree. The
committed `api` snapshots are the oracle:

- every signature `overview` or `ops --sigs` prints appears **verbatim** in that
  fixture's snapshot;
- every `type <Name>` body equals `renderTypeDef` of that declaration exactly;
- every declaration resolves through `type`, and nothing outside the index does;
- every path the tree offers is reachable by `ops`;
- every `--deps` closure terminates and repeats no declaration.

`test/register.test.ts` is the mechanical form of the two-registers rule: no
report document carries a line that reads as a declaration outside a fence, no
`//` annotation outside one, and no code document carries a fence, a table or a
format marker. Without it the two registers drift back together.

`test/symbols.test.ts` carries a **discovery corpus** — every distinct lookup the
nine recorded playground runs made, pinned with its hit count, so a zero-hit pin
cannot masquerade as a working index.

`test/corpus.test.ts` renders nine recorded Central payloads and diffs each
against a committed snapshot. Fixtures span **shape space**, not popularity:

| fixture | what it pins |
|---|---|
| `ballerina/http` | scale, the generic-service injector, annotations, 85 classes |
| `ballerina/graphql` | generic service + the ErrorDetail patch |
| `ballerinax/github` | 903 resource functions, 21,818 lines of output |
| `ballerinax/googleapis.gmail` | cross-package type notes, optional fields |
| `ballerinax/googleapis.sheets` | the Range 2D-array patch, enums |
| `ballerinax/kafka` | listener + service template (`service X on new Y(…)`) |
| `ballerinax/postgresql` | client + driver, 125 classes, non-remote client methods |
| `ballerinax/sap` | the injected-typedef patch, a tiny module |
| `ballerinax/slack` | the OkTrueDef patch, 174 resource functions |

Fixtures are stored gzipped (12MB → 332KB for `github`) because nobody reads a
diff of minified JSON; snapshots are plain text because the diff **is** the
artifact.

`test/keyspace.test.ts` snapshots the payload's whole key space, which is how a
Central field that appears or vanishes becomes a reviewable diff rather than a
silent behaviour change. Re-record and review it after refreshing fixtures:

```bash
pnpm record-fixture ballerinax/kafka          # add or refresh one package
AEP_BAL_UPDATE_KEYSPACE=1 pnpm test           # accept a reviewed shape change
UPDATE_SNAPSHOTS=1 pnpm test                  # accept reviewed overview/ops changes
```

The overview and ops snapshots are rendered under a **fixed** provenance header,
because the real one is run-order-dependent — the same command prints `central`
then `cache` — and a snapshot encoding the live value would fail on its second run
rather than on a regression.

Design notes and the fork decision: [`design/`](design/).
