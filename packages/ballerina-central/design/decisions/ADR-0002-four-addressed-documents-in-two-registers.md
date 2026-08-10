# ADR-0002 — Four addressed documents, in two registers

**Status:** Accepted · shipped 2026-08-10 · partly supersedes
[ADR-0001](ADR-0001-a-typed-fork-of-the-central-reader.md) §5 and its Consequences.

## Context

ADR-0001 shipped one document per package and told the skill to grep it. Nine
recorded playground coding runs say what that cost. The golden one is `bal-stars`
`2026-08-09T16-47-22-542Z-code`, bash calls 8 to 19: one feature — read a
repository's star count through `ballerinax/github`, handle the HTTP error — took
**12 bash turns, about 27,000 characters of context, and two turns that returned
31 and 13 characters of nothing** while the agent probed for where a record ended.

Four facts from that corpus drive everything here.

**The episode is a fixed set of five questions.** How do I construct the client,
what is this operation's signature, what does it return, which field carries the
value I want, and how do I branch on the error. Four of the five are answered by a
*name*, not by a line number.

**The cost is navigation, not fetching.** `grep`, `sed`, escaping
`\[string owner\]`, hunting a record's closing brace. The two largest reads were a
13,097-byte client window and an 8,701-byte record read to learn one field name.
One grep failed outright because `\b` after `]` never matches, costing two turns
to recover.

**Discovery was never solved; it was borrowed from the model.** The trace's second
turn greps for `repos/[string owner]/[string repo]` because Claude has GitHub's
REST API memorised. Measured, an agent without that knowledge has nowhere to go:
the roster says `repos(421)` and a substring search for `repos` returns **484
hits**. Any design whose walkthrough assumes the path is already known is
measuring the wrong episode.

**Error handling was unlearnable.** All 56 of `ballerina/http`'s error
declarations rendered as `type X error;`, so `e.detail().statusCode` — the
most-traced lookup in the corpus, wanted by eight of nine runs — was unreachable
from the document. Fixed separately; it is what makes `type <Error> --deps` worth
having.

## Decision

Four verbs, verb-first, over one cached payload
([ADR-0003](ADR-0003-a-raw-payload-cache-keyed-by-coordinates.md)).

```
bal-library <pkg> [version]                            overview (default verb)
bal-library overview <pkg> [version] [--client C]      same, explicit
bal-library ops  <pkg> [path] [--client C] [--sigs]    operations: navigate, or dump signatures
bal-library type <pkg> <Name>... [--deps]              declarations, as Ballerina
bal-library api  <pkg>                                 the whole API document
```

**A first positional containing `/` is a package; otherwise it is a verb.**

### Why verbs, and why leading

Four distinct nouns should not be four modifiers on one command. An earlier draft
rejected subcommands on a version-skew argument, and that argument was about a
verb placed *after* the package, where a stale binary reads it as a version and
reports `package-not-found` at exit 1 — which the skill teaches means "retry". A
**leading** verb is safe, and measured against the shipped binary: it has no
slash, fails the qualified-name regex, and exits 2. Combined with rejecting
unknown flags, every form of a version-skewed call now fails loudly instead of
misreporting a Central outage. Verb-after-package stays rejected.

### Two registers, never blended

A document either **is** Ballerina or it **describes** a package. Mixing them
produces things like a `client class Client {` shell whose body is
`// WARNING: 903 resource functions` — a document that looks like a declaration,
is not one, and invites a reader to transcribe from it or to reason about what
"the file" contains.

| document | register | format |
|---|---|---|
| `overview`, `ops` | report | Markdown |
| `type`, `api` | code | raw Ballerina |

- **In the report register, Ballerina appears only inside a fenced
  ` ```ballerina ` block.** A fence is unambiguously a quotation, so a signature
  stays copyable truth while the surrounding document cannot be mistaken for
  source.
- **`//` annotations are legal only in the code register**, where they annotate
  real declarations. That is what `// Special Agent Note:` already does. In the
  report register they were the thing doing the impersonating.
- **Structure is Markdown headings**, so `grep '^## '` returns the sections.
- **Ballerina doc comments (`#`) stay inside the fences**, because they are the
  language's own doc syntax and belong to the quoted declaration.

`test/register.test.ts` enforces all of it mechanically, over every fixture and
every verb, so it also covers documents nobody has written yet. Without that test
the two registers drift back together.

### What `overview` carries, and the one type family that stays

Readme, every client's constructor and function signatures, module-level
functions, and **error declarations**. No other types: they are 738KB of
`ballerinax/github`'s 927KB, and excluding them is what makes one document viable.
Every other type arrives named in a return type or a parameter.

Errors are the exception because they are the only declarations unreachable from a
signature. `ballerinax/github` declares **zero** errors and every one of its 903
operations returns the language-level `error`, so nothing in the API document names
`http:ClientRequestError`. They are also cheap — 3,089 bytes for http's 56, at most
334 for anything else, and the section is omitted for the four fixtures with none.

Measured document sizes, rendered under a fixed header:

| fixture | overview | ops |
|---|---|---|
| `ballerina/http` | 45,184 | 2,261 |
| `ballerinax/postgresql` | 26,841 | 442 |
| `ballerinax/googleapis.gmail` | 22,958 | 558 |
| `ballerina/graphql` | 21,288 | 492 |
| `ballerinax/googleapis.sheets` | 17,218 | 457 |
| `ballerinax/kafka` | 17,009 | 505 |
| `ballerinax/slack` | 14,875 | 9,351 |
| `ballerinax/sap` | 11,143 | 2,959 |
| `ballerinax/github` | **7,550** | 1,240 |

`ballerinax/github` — the largest package — has the *smallest* overview, because
the listing rule replaced its 903 operations with a tree. The largest is
`ballerina/http`, because the rule is **per client** and it has eight, none of
which individually exceeds either limit. `--client` narrows it, and the skill says
never to look `http` up at all. For everything else the driver is the readme:
postgresql is 23.6 of 26.8KB guide, which is the right trade, because that is the
"how is this used" answer and the reason the traces never found it.

### The listing rule

Remote and normal functions are always listed in full. Resource functions are
listed in full **unless the client exceeds 100 operations or 20KB of signatures**,
whichever comes first, in which case the path tree is printed with a stated
warning.

The byte guard matters as much as the count: at gmail's measured ~480 bytes per
operation, 100 operations is about 48KB, too large for a document meant to be read
from the top. On this corpus both halves agree — github and slack take the tree,
gmail at 32 operations and 15.3KB stays inline — so the guard costs nothing today
and stops a verbose 80-operation connector from blowing up.

### Path matching is anchored, segment-wise, from the first segment

Not a suffix match and not a substring match. Measured on github, for
`repos/{owner}/{repo}`:

| match | operations |
|---|---|
| anchored (what ships) | **3** |
| suffix | 9 |
| substring | 426 |

The nine include `orgs/{org}/teams/{teamSlug}/repos/{owner}/{repo}` and
`teams/{teamId}/repos/{owner}/{repo}`, which are about team access rather than
repositories. A caller that asked for one path and got other subtrees mixed in has
nothing in the output to tell them apart, which makes substring matching a
correctness bug rather than a convenience — and the reason `ops` walks a tree
instead of filtering strings.

### Auto-descent names what it skipped

`ops <pkg> repos` steps down through levels that add no routing choice and reports
the branches it did not take. Not cosmetic: `repos` genuinely has two parameter
children with different spellings — `{owner}` with 420 operations and
`{templateOwner}` with 1 — and collapsing silently to the dominant one hides an
operation permanently, because nothing downstream would mention it again. The
sibling is reported at its full path (`repos/{templateOwner}/{templateRepo}/generate`),
because naming a branch without naming where it goes makes the skipped operation
harder to reach than before it was mentioned.

Only **parameter-only** levels are stepped through. A level with a literal child is
a real choice about which resource to address.

### Two spellings for a path segment

Central publishes github's paths as Ballerina writes them: `code\-scanning`,
because `-` needs escaping in an identifier, and `'import`, because `import` is a
reserved word. Those are correct inside a fence, where the line is a quotation of
source, and unusable in prose or as a shell argument. Prose and navigation use the
readable spelling; matching accepts both.

### `type` is all-or-nothing, and matching never picks silently

`type` takes several names. **If any one fails to resolve, nothing is written to
stdout** and one `symbol-not-found` object lists every unresolved name with
candidates ranked by the longest run of characters shared with the request. That
preserves "exit 0 means stdout is complete", which every redirecting caller relies
on.

Name matching is **exact first, normalised equality on miss** (letters and digits,
lower-cased), and **more than one normalised match is a failure with all matches
listed**. Not theoretical: github's `ManifestConversions` declares both `clientId`
and `client_id`, and `ballerina/http` has 61 constant-versus-class collisions of
the `STATUS_ACCEPTED` / `StatusAccepted` shape.

`--deps` appends the transitive **same-package** closure, depth-first so a chain
reads as a chain, and names cross-package edges in a footer with the exact
follow-up command. `http:ConnectionConfig` decides that policy: its local closure
is **one** and everything of interest lives in another payload, so crossing the
boundary would hide a five-second cold fetch inside an answer the caller expects
to be warm.

### `symbol-not-found` is exit 2

Both its recoveries — a different name, or `--refresh` — are edits to the caller's
argument list, which is `exitCodeFor`'s stated rule. It keeps its `1 | 2` return
type and the two-code contract stays written in four places.

## The measured result

| | turns | context | fetches | needs model knowledge |
|---|---|---|---|---|
| before | 12 | ~27,000 | 2 | yes, to write the greps |
| after | **4** | **~20,000** | 2 | **no** |

Verified end to end against live Central: `overview` (7,543 B, 5.06s cold),
`ops repos` (2,991 B, warm), `type FullRepository` (8,765 B, warm),
`type ballerina/http ClientRequestError --deps` (782 B, cold).

The dependency order is checked rather than assumed. T1 → T2 because the tree is
the only thing that supplies `repos`. T2 → T3 because `FullRepository` comes from
T2's **return type**, never from the guide — github's own readme example returns
`Repository`, and eleven records in the package carry a `stargazersCount`, four of
them optional, so reading the field off the wrong record compiles into a
nil-handling bug. T4 is supplied by nothing upstream, which is why the overview
carries the Errors listing.

## Rejected

| option | why not |
|---|---|
| A line-range index prepended to `api` | Keeps grep-then-sed in the loop, rests on line arithmetic that is silently wrong when off by one, and would index the 70 github declarations that render as `// Unknown type: X`. |
| **Reordering `api` so clients precede types** | Moves every declaration in all nine snapshots, contradicts the order `render/document.ts` declares as the output's contract, and does not solve the motivating case — github's client section is 2,715 lines on its own. **This explicitly supersedes ADR-0001's Consequences.** |
| A general text search verb | Discovery is served by the path tree and the overview's listings. A substring search returns 484 hits for `repos` and 62 for `error` on `ballerina/http`, and any cap over those is an arbitrary truncation of the answer. |
| Doc-comment search | Measured: the natural term `star count` returns exactly one confident hit, and it is a query-parameter enum rather than the field. A plausible wrong hit costs a build cycle where an honest absence costs a turn. |
| Single-field addressing (`Type.field`) | Reading a record whole is 8,765 bytes for github's worst case, the agent needs the surrounding fields anyway, and a field slice needs an elision convention that is itself a small lie about the declaration. |
| Wire-name annotations in the rendered record | Not unwanted, impossible: `stargazers_count` occurs **zero** times in the 12.4MB payload. Central publishes the annotation's presence and never its value. |
| A type roster in the overview | 33,431 bytes for github's 1,224 names, and no traced run ever needed to discover a type name it had not been handed by a signature. |
| A cap on `ops --sigs` | It hands back 90KB for `repos` if asked. The tree already showed `repos 421` and the header leads with the count and byte size, so the caller chose it with its eyes open. A truncation that can drop the answer is worse. |
| Restoring the `.bala` disk fallback | The error fix is what makes it unnecessary, and its own mirror-image grep failure — `resource function` never matches `resource isolated function` — burned 6, 5, 3, 2 and 1 consecutive dead turns across five recorded runs. |
| A `--required` view of a config record | Central publishes no `defaultValue` on inclusion members and the transform drops `isOptional` for them, so `http:ClientConfiguration` computes as 18-of-19 required when almost all are optional. |
| A `--json` output mode | The consumer writes Ballerina. The code register already has a byte-exact oracle and the report register is for reading. |
| A CLI framework (commander, yargs, citty) | What one adds is help generation and its own error printing, and this command owns both: the usage text carries the resolved cache directory, and every failure must leave stderr holding exactly one `Failure`. `node:util`'s `parseArgs` covers the rest with no dependency in an image on the agent's critical path. |
| Keeping `--readme` | The guide is the overview's last section, so a readme-only document would be a second way to ask one question. With unknown flags rejected, a stale `--readme` fails at exit 2 rather than resolving as a version. |
| A verb after the package | A stale bundle reads it as a version and reports `package-not-found` at exit 1, which the skill teaches means "retry". |

## Consequences

- **The default document changes shape.** An agent running the old `SKILL.md`
  against the new binary gets Markdown where it expected 21,818 lines of
  Ballerina. `api` exists so a stale instruction is recoverable rather than fatal.
- **The skew is detectable rather than silent.** Every report document opens with
  `<!-- bal-library <verb> v1 -->`: it survives `grep`, renders as nothing, and
  cannot appear in the old document. The binary ships baked into the runner image
  (`build-runner.sh` skips the build when the tag exists, so a stale image is the
  normal case) while `SKILL.md` ships through the skills git mirror, so "they land
  together" is not achievable. **Land the binary first, force-rebuild and verify
  the runner image, then let the skill edit reach an org.**
- **The provenance header makes stdout run-order-dependent.** The same command
  prints `central` then `cache`. Snapshot tests pin the body under a fixed header
  rather than pretending the line is deterministic.
- **`ops` and `type` disagree about a missing thing.** A `type` name that does not
  resolve is exit 2; an `ops` path that does not exist is exit 0 with the available
  segments. The split is defensible — an empty path is a fact about the tree, a bad
  name is a bad argument — but it is the part of the contract an agent is most
  likely to invert.
- **The committed snapshot surface grew** by 18 view snapshots plus the discovery
  corpus, making every future rendering diff larger. That is the point: a rendering
  change should be a reviewable diff rather than something an agent finds at run
  time.
- **`--project-dir` now matters on every verb.** Without it, `type <pkg>
  ConnectionConfig` returns Central's latest while `bal build` compiles the pinned
  version, and the agent writes a field that does not exist with no way to see why.
