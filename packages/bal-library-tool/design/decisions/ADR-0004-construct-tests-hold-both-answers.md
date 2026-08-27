# ADR-0004 — construct tests hold both answers, and coverage is a map rather than a gate

**Status:** Accepted

## Context

A fidelity audit compared the reader's output against Ballerina Central for twelve packages, construct
by construct, and found 60 distinct defects: closed records rendering as open, `arrayDimensions` never
read so 2-D arrays come out 1-D, `&` printed as `|` on 17 public error declarations, 340 class and
object-type declarations rendered as an empty body, `table<T>` losing its row type. The fixes are staged
and touch the same three files repeatedly, so each stage has to change one family of constructs and
leave the rest alone.

Two things about the existing suite made that unsafe.

**Coverage was already high and said nothing.** Measured with JaCoCo: **93.5% instruction, 79.2%
branch** before any of this work. The defects are not on unreached lines. `Schema.java`'s
`cursor.array(json, path, "classes", Schema::named)` executes for every one of the nine fixtures and is
precisely why a class comes out as `class X {\n}` — a covered line producing a wrong answer. Adding 64
construct assertions afterwards moved instruction coverage by **0.0 points**. Coverage measures whether
a line ran, and every defect here is a line that ran and was wrong.

**The corpus could not localise a change.** `CorpusTest` renders nine recorded packages and diffs
whole documents. It is a genuine oracle — the payloads and `.bal` snapshots came from the TypeScript
reader this was ported from — but a one-character fix to the `stream<A, B>` separator fails it as three
package-level diffs (`graphql`, `http`, `postgresql`) plus two view tests, none of which name the
construct. And a construct no recorded package happens to use is not tested at all.

## Decision

**Add a construct table: one synthetic Central payload per Ballerina syntax dimension, run through the
real pipeline, with each case recording BOTH what we print today and what the construct's own
declaration is.** Keep JaCoCo, wired as a floor set below current reach rather than as a target.

### Both answers, in the case

`Construct` carries `renders` (today's output, verbatim) and `shouldRender` (the correct Ballerina, or
`null` where they agree). The assertion is `actual == renders`. What each failure means:

| the case | what happened | the message says |
|---|---|---|
| no open finding, output changed | a regression on something that was right | **A REGRESSION** + first-difference diff |
| open finding, output changed to `shouldRender` | the fix landed | **A FIX LANDED** — promote the row, tick the finding off |
| open finding, output changed to something else | a regression on something already wrong | **A REGRESSION**, with all three texts |

Recording only the correct answer was the obvious design and it is wrong: 37 of the 64 cases would fail
on a green checkout, so the suite would be disabled or the assertions weakened. Recording only today's
output is worse in a subtler way — the suite goes green after a fix and never says the fix arrived, so
nothing connects a code change to the defect it was supposed to close. Holding both makes a clean
checkout green *and* makes a landing fix loud.

### The payload shapes are copied, not invented

Every case's JSON is the shape Central really sends, at the depth it really sends it: `isClosed` on the
record, `isReadOnly` on the field, `isRestParam` on the field's *type* node with the field's `name` an
empty string, `arrayDimensions` beside `isArrayType`, an error's base and its detail record both under
the single `detailType` key. Invented shapes would let a case pass while testing something Central never
publishes, which is the failure mode a hand-written payload has and a recorded one does not.

### One construct per case, scoped where a package name leaks

The package is `test/pkg` so no per-package entry in `Patches` applies. Three cases need a real name to
reach a patch, and naming `ballerina/http` also gets http's service injector — so a case can scope
itself to one document section. Without that, an error case would fail when the service template
changed, which is exactly the cross-talk the table exists to remove.

### The coverage floor is 80/70, below the 93/79 the suite reaches

A floor at current reach fails on the first refactor that deletes a covered branch; a floor above it
fails on day one. Set below, it only ever fires on a real regression, and the report stays what it is
useful as — a map of what nothing executes.

## Consequences

`./gradlew :native:check` now enforces the floor. `jacocoTestReport` runs after `test`.

**The construct table is the wrong tool for a third of the register.** It pinned 33 of the 60 defects
when it was written. The rest are properties of a view rather than of a construct — `overview`'s
declaration counts, `ops`'s wildcard, the cross-package footer's coordinate, the readmes — and stay
with `ViewsTest`, `ViewsAgreeTest` and `SymbolsTest`. `ConstructTest` asserts the pinned set by name
so that boundary is visible rather than assumed.

**Landing a fix means editing the table, on purpose.** Promoting a row is a two-line diff a reviewer can
check against the register. The alternative — a switch that rewrites expectations — is what the nine
`.bal` snapshots deliberately do not have, and for the same reason: these are the assertions that say
what Ballerina is, and transcribing them from the output would make them agree with whatever the code
does.

Stage 0 of the fix plan exercised the whole lifecycle and it worked as designed: four cases failed
with **A FIX LANDED** naming the finding, were promoted, and now guard the right answer instead of the
wrong one; the pinned set is a diff, 33 names down to 29. The one case that mattered most was one
nothing was expected of — `errors/detail-restored-for-http`, which caught a patch whose regex matched
the *old* spelling of an intersection and would otherwise have stopped restoring `<Detail>` in
silence. That is the failure the corpus cannot localise and the reason the table exists.

**Three findings sat on constructs the corpus also pinned wrongly**, and both are now gone:
`PatchesTest` keyed on a positional field write, and the committed `ballerinax__googleapis.sheets.bal`
snapshot checked in a wrong `values` line. The construct table did not fix those; it made the accompanying
failure legible, which is all it claims to do.

**Two ways a case can be wrong that this table does not catch, both found in practice.** A case can record a
`shouldRender` that its own payload does not imply — `services/generic-injected` expected a service path and
a resource method appearing nowhere in its payload, so when the fix landed the suite reported
`A REGRESSION` on a construct that had actually become correct. And a case can be pointed at a package where
the mechanism under test cannot fire: the detail-argument case used the default synthetic package, and since
no reader can recover an argument the payload does not carry, it could never have gone green whatever landed.
Both failure modes look like a red suite rather than a wrong expectation, so the rule is to re-derive the
expectation from the case's OWN payload before believing the verdict.
