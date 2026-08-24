# ADR-0007 — a per-package correction must name the fact Central drops

Status: accepted · 2026-08-12

## Context

`Patches.java` held eight per-package corrections, each keyed on a library name, each with a javadoc
explaining why the package needed special handling. Every one of them was written in good faith against a
real symptom. Re-deriving all eight against the payload and the packages' own published sources — which is
what the fidelity register's stage 4 is — five did not survive:

- **Two were inert.** The reader had grown to handle the shape, and the correction went on matching nothing.
  Neither failed, because each was pinned by a test asserting the OUTCOME (a 2-D array; a closed record)
  rather than the mechanism, and the reader satisfied the outcome on its own.
- **One injected a name no oracle declares.** `RequestMessage` is `ballerina/http`'s, not
  `ballerinax/sap`'s; all eight of sap's use sites already printed `http:RequestMessage`, and the injection
  rendered as `// Unknown type: RequestMessage` — the placeholder its own finding complained about.
- **One rewrote 179 field types on a style judgement.** Slack declares `public type OkTrueDef true;` and
  writes `OkTrueDef ok;` 179 times. The correction replaced the alias with the literal because the alias
  was "not worth finding", which diverged from the source at 179 lines and made a published declaration
  unreachable through the tool's own name lookup.
- **One replaced seven real declarations with three comment lines**, on the premise that Central described
  no service for `ballerina/http`. It publishes one listener and seven `distinct service object` types.

And a sixth was hiding a public API: `removeChatClient` dropped `ballerina/ai`'s `ChatClient` as "an
internal detail … the largest client in the module". Central publishes three clients for that module and
`ChatClient` is the smallest.

The pattern is not carelessness. It is that "this package's docs look wrong" and "Central omitted something"
feel the same at the point of writing, and only the second is a fact anyone can check later. A correction of
the first kind has no failing state: there is nothing it can stop being true about.

## Decision

A per-package correction is admitted only if it can state all three of:

1. **The fact Central drops.** Named, and located in the payload as an absence — a category that does not
   list the declaration, a key that does not exist on the item.
2. **The oracle that has it.** The package's own published source, at a file and line, from the same version
   the fixture records. Not a different version, and not inference from a rendered doc page.
3. **A pin in both directions.** What it must change, AND what it must leave alone. Both, because a widened
   correction over-reaches and a narrowed one silently stops firing.

Two things that are explicitly not grounds for a correction: that the package's published API is awkward, and
that our output would read better otherwise. Neither is something Central got wrong.

Where the correction is a hand-maintained table, the pin covers **every row and the row count**. The
detail-argument table's predecessor is the worked example: it matched a rendered intersection, a stage
respaced `&`, the pattern stopped matching, and the only test asserted that a `Detail` record was reachable —
which stayed true. Eight of eleven sites went uncorrected with nothing red.

## Consequences

- Three corrections remain, from eight: the detail-argument table (11 rows across 3 packages), one injected
  declaration Central has no category for, and one module path that needs quoting because `client` is a
  keyword. `Patches.java` is 250 lines, from 333.
- Retiring corrections retires the IR shapes that existed only to carry them. `TypeDef.Other` had one
  producer and went with it; `Service` was a sealed pair whose second case a patch produced, so it collapsed
  to a record; `mapRecord` and `RecordField.withType` lost their last callers. Coverage went up, because what
  was deleted was the part no test exercised.
- The bar applies to the corrections that stay. `changeClientConfigName` was verified by the compiler and
  guarded by nothing — no fixture covers `ballerinax/client.config` — so it now has a test that calls
  `applyPatches` on a hand-built library, since the fact under test is about a NAME and needs no payload.
- The cost is that a genuine correction takes longer to admit: it needs a compile proof or a source citation
  before it can land, not a symptom. That is the intended trade. Five of eight would have cleared a lower
  bar, and each one made the document less true about the package it described.
