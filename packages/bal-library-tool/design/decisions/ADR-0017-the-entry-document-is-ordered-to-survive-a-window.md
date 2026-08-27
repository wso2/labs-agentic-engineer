# ADR-0017 — The entry document is ordered to survive a window

Status: accepted, implemented. Two later decisions build on this one and are folded in below where they
change it: the 2026-08-17 interface change made `overview` a bounded MAP that generates no signature at
all, and [ADR-0024](ADR-0024-the-quickstart-is-every-ballerina-block.md) replaced how the quoted readme
code is selected and where it sits.

## Context

`overview` appended the package's whole readme so that an agent always had a worked example and
the package's own practices. Measured over the eleven packages the 2026-08-15 eval sweep touched
— 4,168 rendered lines — it almost never arrived.

| window | constructor | first worked snippet | all snippets | `## Next` |
|---|---|---|---|---|
| `head -50` | 10/11 | **0/11** | 0/11 | 0/11 |
| `head -100` | 10/11 | **0/11** | 0/11 | 0/11 |
| `head -150` | 10/11 | 4/11 | 0/11 | 0/11 |
| `head -200` | 10/11 | 5/11 | 3/11 | 3/11 |

80% of the `bal library` calls in that sweep were piped through a filter, and two independent
statements of a "run it unpiped" rule — the skill's and `--help`'s — moved that zero percent.
So the document has to survive a window rather than forbid one.

Three more measurements decided the shape:

- **The guide was 44% of the corpus and its code was 12%** — 1,820 lines carrying 412 of code.
- **29% of the guide, 519 lines, was `Setup guide` / `Prerequisites`**: create a GCP project, buy a
  Twilio number, click through an OAuth consent screen. Those 519 lines contained **four lines of
  code between them.** No coding agent can act on any of it.
- **The code is load-bearing when it arrives.** kafka's snippet is the only place `acks: "all"`
  appears; slack's is the only place the `->/chat\.postMessage.post(…)` call form appears, and its
  absence caused both of the sweep's signature errors; slack's also carries
  `configurable string token = ?;`, the required-configurable syntax whose absence cost one case
  five compile errors.

So the readme's value was concentrated in about a fifth of its lines, and the other four fifths
were what pushed that fifth past every truncation point.

## Decision

**The entry document leads with what is load-bearing and ends with what is unbounded.** In order: the
facts table, the map, `## Next`, and the quoted readme code last.

1. **The readme is split rather than appended: its code is quoted in this document, its prose becomes a
   verb.** Which blocks are quoted, and their position last, are ADR-0024's — this ADR's own
   connector-shaped selection rule and 40-line cap are what that one reverses. What holds from here is
   the split itself: the code is what a coding agent can act on, and the prose is what pushed it past
   every window.

2. **`guide` becomes a verb.** The whole readme, verbatim, addressable one code-carrying section at a
   time so a caller can open the part the map points at. A verb rather than an `overview --guide` flag:
   ADR-0002 is verb-first with no implicit default, and this is a document Central publishes rather than
   a mode of ours. The setup steps stay reachable for the reader who came for them.

3. **`## Next` moves back above the bulk.** It was moved last on the argument that "the guide is the
   largest section for most packages"; the guide left, and the argument with it.

4. **`## Errors` and `## Configurables` leave the document.** Errors were 292 lines and the second
   largest section, and `type` returns the same declarations byte for byte, several names at a time
   — so the section goes and the NAMES stay on the facts row, because
   `BucketAlreadyOwnedByYouError` is not guessable and a bare count is a reference the reader cannot
   act on. Configurables appear in one package of eleven and their own text says they are
   module-private and cannot be referenced from code; they stay in `api` and are simply not in the
   document an agent writes `.bal` files from. The subtype-chain reading rule moved to `type` with
   the declarations (ADR-0013), or the move would have deleted it.

5. **`overview` generates no signature at all — it is a bounded map** (2026-08-17). That is a stronger
   form of this ADR's goal, not a reversal of it, and the ordering above is untouched. Ordering alone
   left the document's SIZE a property of the package: `ballerina/crypto` reached 1,177 lines and 64,310
   bytes, overflowing an eval harness cap that then silently substituted a 2.2KB stub — the failure this
   ADR was trying to prevent, arriving through the one door ordering does not close. A byte cap was
   considered and rejected: it would still let crypto emit 20,000 bytes before degrading. A map is
   bounded by CONSTRUCTION, so crypto is about twenty lines and the corpus fell from 2,426 lines to 732.
   The cost is stated rather than hidden: `googleapis.sheets` goes from one call to two, because its
   208-line overview carried all 43 remote signatures. That is why the 20,000-byte signature budget
   moved onto the container verbs (ADR-0020) — the second call returns all 43 in full — and
   `ViewsAgreeTest` asserts the map generates nothing so this cannot drift back.

## Consequences

Measured live against the same eleven packages after the reordering:

| window | `### Constructor` | a construction snippet | first snippet | all snippets | `## Next` |
|---|---|---|---|---|---|
| `head -50` | 2/11 | **10/11** | **10/11** | 8/11 | 7/11 |
| `head -100` | 9/11 | 10/11 | 10/11 | **10/11** | **11/11** |
| `head -150` | 10/11 | 10/11 | 10/11 | 10/11 | 11/11 |

**4,168 → 2,426 lines (−42%), 206KB → 125KB (−40%). Documents over 200 lines: 8 → 4.** The one
package that never reaches 11/11 is `ballerina/uuid`, whose guide contains no fenced code at all;
there is nothing to quote and nothing was lost. `ballerinax/postgresql` — 91% guide — goes 618 → 121
lines, and slack's call form moves from line 169 of 193 to line 30 of 106.

Two costs, taken deliberately:

- **The formal `### Constructor` signature left `head -50`** (10/11 → 2/11), and the map removed it from
  the document altogether. What replaced it is a worked construction with real field names, at 10/11,
  and the `Document` row says how long the rest is. An agent that wants the declaration rather than the
  example pays one `client` call.
- **A configurable is now reachable only through `api`.** Unlike the errors, `type` cannot resolve
  one — `type ballerina/http maxActiveConnections` answers `symbol-not-found`, and `ViewsTest` pins
  that so the cost stays visible. For a thirteen-entry pool-tuning list in one package of eleven
  that is the right price.

One behavioural claim remains an assumption, and the falsifier is stated so a later sweep can settle it:
that an agent runs `bal library guide <pkg>` when it needs setup context. **If lookups per case rise by
more than one while build cycles do not fall, the guide pointer is not being followed and the quoted code
is not carrying enough — widen the budget before adding prose back.**

## Alternatives rejected

- **A pure umbrella** — facts and counts only, everything else addressed. Bounded, and it would work
  for an agent that asks. The sweep says agents do not ask for what they do not know they need:
  across 74 calls `api` was used **0 times** and no agent ever ran a verb-level `--help`, which is
  where the `--version` flag that would have saved seven calls is documented. A pointer is reliable
  for a question the reader already has and unreliable as a substitute for content they did not know
  to want. So the code is inlined and the readme's *prose* becomes the pointer, because a reader who
  wants setup steps knows they want them.
- **Deleting the readme outright.** postgresql's is 563 lines and a genuine usage manual: connection
  pooling, SSL, CDC. That is the case `guide` exists for. A code-only extract was rejected for the same
  reason: `googleapis.sheets`' readme is 178 lines with 4 code blocks, so extracting code alone discards
  about 85% of it — including "if you intend to use the `deleteSpreadsheet` operation, you must also
  enable the Google Drive API", which is not inferable from any signature and is the difference between
  a connector that works and one that 403s on one operation.
