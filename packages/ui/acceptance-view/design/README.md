# Design notes — `@aep/ui-acceptance-view`

This package renders `specs/validation/acceptance/*.feature` — the Gherkin acceptance
criteria — optionally joined against a run's `tests/acceptance/report.json`. Two
consumers, one component:

| Consumer | What it passes | What the reader is doing there |
|---|---|---|
| Spec view's file pane (`SpecView.tsx`) | `features` (one file) | Reading the document, before any run exists |
| Validations page (`ValidationPage.tsx`) | `features` (all of them), `report`, `awaitingReport` | Reading run results |

ADR-0029 shipped the Gherkin path and deferred this deliberately — *"the console
renders the report raw … a shaped view is real design work, and the right time
for it is after reading a real report."* This is that work, designed against the
p55/p56 reports and the `shopping-list` playground pair.

## One tree, two sources

The feature file and the report describe the same thing:

```text
Feature: Bought items
└─ Rule: A bought item is locked from further edits      @story-6
   ├─ Scenario: Marking an item bought                    → Passed
   └─ Scenario: Editing a bought item is refused ⊖        → Blocked
      ├─ Given the list has a bought item named "Eggs"
      ├─ When Dev tries to change the quantity            ← the blocking reason
      └─ Then the quantity is still "1"                   (not reached)
```

The feature file supplies the **structure** — features, rules, scenarios, steps,
tags, line numbers. The report supplies, per scenario, an **outcome**, and per
step the **evidence**: `command`, `exit`, `observed`. So this is one renderer
with an optional prop, not two components.

**The features are the spine, not the report**, and that is the load-bearing
choice. Rendering from the report alone would be simpler and would make DRIFT
INVISIBLE: the features are read at the branch tip and the report at the merge
commit of the attempt that wrote it, so a scenario authored since has no entry.
That is the ordinary authoring loop — read a failure, ask the agent for one more
scenario — and it has to show. It gets a neutral `No result`, for the same
reason `validation-view` gives one to a drifted criterion: colouring the expected
state teaches a reader to discount the colour.

## The vocabulary is the report's own

`outcomes.ts` title-cases whatever word the report carries. There is no
translation table, so there is no fifth vocabulary to keep in step with
`report.go`, `check-report.mjs`, the skill and the ADR — and an outcome word the
console has never heard of renders verbatim and neutral rather than falling
through to a wrong label. That agrees, without either side knowing about the
other, with the Go ladder counting an unrecognised outcome as a gap rather than
as coverage.

Tone and mark DO need a table, and at four outcomes they need one more than they
did at two. ADR-0016 gave `Passed` and `Failed` marks because as outlined chips
they otherwise differ by hue alone; adding `Blocked` and `Unjudgeable` creates
two more such pairs, so every outcome carries its own glyph.

| Outcome | Tone | Glyph | Why |
|---|---|---|---|
| `passed` | success | `Check` | |
| `failed` | error | `X` | |
| `blocked` | **warning** | `Ban` | Amber means A PERSON HAS TO LOOK. |
| `unjudgeable` | **warning** | `CircleHelp` | The same, and they differ by glyph. Split once — blocked amber, unjudgeable neutral — on the claim that blocked was the only row with a call to action. The code disagrees three times over: `FailedScenarios` returns `failed` alone so NEITHER files a repair issue, the Go ladder pairs them in one arm, and the partial sentence counts them together. |
| *(absent)* | default | — | `No result`: the console's own word, because the scenario is absent from the report and the report has none for it. |

## One card, a toolbar and a scrolling list

The layout follows a reference widget a UI designer produced. Its geometry is
reproduced exactly — radii, paddings, sizes, weights — while every colour goes
through the Oxygen theme, because the reference is light-only and
`design-system.md` requires both schemes and forbids hex literals. The
substitutions are one-for-one: `#FF7300 → primary.main`, `#181818 →
text.primary`, `#9CA3AF → text.secondary`, `#0277BD → info.main` (already
identical), `#FAFAFA → action.hover`, `rgba(0,0,0,.07) → divider`.

```text
┌─ card ──────────────────────────────────────────────────────────┐
│ [🔍 Filter by scenario, step or capability]     [⤢ Expand all]  │
│ Filters  [All|Passed|Failed|Blocked]  @negative @story-1 …  Reset│
│ 12 of 15 scenarios match                                         │
├──────────────────────────────────────────────────────────────────┤
│ ▾  Bought items                      @story-6 @story-7   4 of 4  │
│      3 rules · 4 scenarios · 2 refusals                          │
│      A bought item is locked from further edits        @story-6  │
│      ▸ Editing a bought item is refused    @negative  [Blocked] 3│
│          the Actions cell is a literal em dash and the row …     │
└──────────────────────────────────────────────────────────────────┘
```

**The card owns the scroll**, not the page, and that is also what keeps the
toolbar on screen. There is no `position: sticky` anywhere in this console and
none is needed here.

**Nothing here describes the RUN.** No tally, no commit, no deploy URL, no
isolation statement: this view renders the criteria and what was made of them,
and the run's identity belongs to the page that owns the run. The tally
especially — `validation-view`'s notes already say why, and this package briefly
broke the rule: *"Counts belong with the verdict that explains them, which the
consumer renders above; a second copy here says the same numbers twice."*
`ValidationPage` prints them once, in its verdict tile.

The isolation statement is the one with a real cost, so it is worth being plain:
the run is still HELD to it — `check-report.mjs` fails a report that omits it —
the reader is simply not shown it. The contract enforces; the page stays quiet.

**`Rule:` survives, as a band inside each capability.** The reference design has
no rule level at all — its sample data has no rule field, so it models generic
Gherkin rather than deciding against ours. In this dialect `Rule:` is required,
carries the `@story-N`, and is enforced to cover every PRD story, so the rule
sentence is the product statement and the only story traceability there is.

**The filters are derived, never fixed.** Outcome options come from the outcomes
the report actually contains, so a run with nothing blocked shows no `Blocked`
segment that could only ever empty the list, and a run where everything came out
the same way gets no control at all. Tags come from the parsed files, `@negative`
first and the stories in numeric order (a string sort puts `@story-10` between 1
and 2). Both mirror the reference's own `ALL_TAGS` derivation.

Three things the filter has to get right, each with a test:

- **Filter on the outcome AS RENDERED.** A scenario the report does not cover has
  no outcome at all; keying on `report.outcome` would drop every `No result` row
  out of every segment, `All` included. `BuildsLedger` documents the same trap
  for its own status filter.
- **Every selected tag, not any.** Two tags narrow; they do not widen.
- **No `No result` segment while an attempt is in flight**, because an uncovered
  row carries no pill then — the segment would select rows showing no state.

**Oxygen's `SearchBar` cannot carry a clear button**, which is why the field is a
plain `TextField`: `SearchBar` hardcodes `endAdornment` *after* spreading
`slotProps.input`, so anything passed in is overwritten with `undefined`, and
the base component that does accept one is not re-exported from the package
root. Both existing `SearchBar` sites in the console have no clear button, which
is why nobody had hit it.

The segmented control's `sx` is lifted from `WireframePanel.tsx`, comment
included: its `height: 28` is exact rather than a floor, because the natural
height jumps the row.

## The row

```text
›  Editing a bought item is refused  @negative        [⊘ Blocked]
   On a bought row the Edit button is not disabled, it is absent — …
```

- **The chevron leads**, on every row including passed ones. It is what a reader
  reaches for, so it earns the left column, and because every row has one the
  column is never slack. `INDENT` is derived from `CHEVRON + the row gap`, so the
  column and everything hanging off it cannot drift apart.
- **A refusal is its own tag**, rendered as the pill the reference design already
  draws for tags. It began as an inline circled minus with visually-hidden text;
  the pill says the same thing in readable text AND doubles as a filter, which a
  glyph never could.
- **The marks flow INSIDE the sentence**, in the same `Typography` as the name
  rather than as a flex sibling, so they follow the last word — onto line two
  when a long name wraps. That is what lets a name never be truncated: a column
  would have to hold its width against a name running to 104 characters, and for
  a specification you want the whole sentence. Right-aligning them against the
  outcome pill buys a column of marks to scan, which is the weaker half of the
  trade now that `@negative` is a filter chip in the toolbar.
- **The refusal mark reads `scenario.negative`, never the raw tags.** `@negative`
  inherits `Feature → Rule → Scenario`, so a scenario under a prohibition rule
  has the property without carrying the tag — 22 of this repo's 77 refusals are
  that shape, and reading the tags showed them nothing. For the same reason the
  rule band and the feature header DROP `@negative` from their own tag lists:
  every scenario beneath them shows it, so repeating it one level up says the
  same thing twice.
- **The two tag kinds differ in FORM, not just colour.** `@negative` is a
  marked pill; `@story-N` is quiet mono text. That lands the right way round —
  one is a property a reviewer scans for, the other a citation pointing at a
  requirement, and a pill would dress a reference up as a discrete object you
  might click. It also survives greyscale and colour-blindness, which a hue
  difference alone would not.

  **`@negative` takes `info`, not the brand accent** — orange says "the
  product's own thing", which a refusal is not, where blue says "an
  informational property", which it is. It shares `info.main` with the literals
  emphasised inside a step, and that is fine: a mark after a sentence and mono
  text inside an expanded box do not read as one signal. **Amber is the hue it
  may never take**, because amber means a person has to look, and a refusal
  scenario is ordinary spec rather than something to act on.

  **What a pill means here is "marked", not "clickable".** The outcome pills are
  pills and are not interactive either; the toolbar's filter chips are the only
  clickable ones, and they are MUI `Chip`s. So the card reads: a pill is a state
  or a property worth marking, text is a reference.
- **No step count.** It held the flush-right column the outcome pill wants,
  answered no question a reader has — a 3-step scenario is not better or riskier
  than a 5-step one — and on a blocked scenario it counted steps that never ran.
  The count that stays is `N of M` on the feature header, which says a filter is
  hiding something.
- **The outcome pill keeps its glyph** although the reference design's is
  text-only. ADR-0016 requires a mark on an outcome so the set can be told apart
  at a glance rather than read one at a time, and four outcomes make that bite
  harder than the two it was written for.
- **Nothing opens by default.** Which makes the one clamped line under a
  non-passed row load-bearing: it is the only thing on the page saying *why*. It
  is the **deciding step's** `observed` — `deciding()` from
  `validation/report.go`, reused rather than re-derived, so the console's summary
  and a repair issue quote the same step.

## The step box

```text
Given   the list has a bought item named "Eggs"
        POST /items
When    Dev tries to change the quantity of "Eggs" to "2"
        agent-browser snapshot -i
        the Actions cell is a literal em dash and the row holds zero
        buttons, inputs or links
Then    the quantity of "Eggs" is still "1"              not reached
```

The box is mono, and the exception is deliberate:

- **step text** — 13px mono, with its literals emphasised. A Gherkin step is a
  sentence with data embedded in it, and the data is what makes it falsifiable;
  the authoring skill insists on concrete values for exactly that reason, so a
  reader scanning a spec is looking for them.
- **command** — 11px mono, clamped to two lines, full text on hover. A real one
  runs to 400 characters of `--fn` predicate, and it is often not a command at
  all (`n/a`, `(already signed in from prior scenario)`), so it is never dressed
  as a terminal. Provenance, not payload.
- **`observed`** — 12px in the **body face**, never clamped. THE payload, and the
  one thing in the box nobody typed: it is a sentence the agent wrote, and on a
  blocked step it runs past 400 characters. Prose that long set in a monospace
  is what makes a log unreadable.
- **`exit` shows only when present AND nonzero.** Absent is not zero: the Go
  struct uses `*int` for this, and a blocked `Then` has neither. There is no
  per-step success tick either — a green mark on every step would put one on the
  very step that blocked a scenario, whose command succeeded at proving a control
  was absent.
- **Steps the report does not reach render de-emphasised, `not reached`**, so a
  blocked scenario visibly STOPS partway. That is the story the report tells and
  the raw JSON hides.

**The emphasis is a regex, not a grammar.** `prismjs` ships a complete Gherkin
language and is already in the tree — but only transitively, through an Oxygen
component nothing here uses, so reaching for it means promoting a transitive
dependency to a direct one. It would not help anyway: that grammar is anchored
on `^Feature:` / `^Scenario:` / `^@tag` and has no notion of a bare step line,
which is all `tokenize.ts` is ever handed.

## The join is identity, never the line

`scenarioKey` is feature + rule + scenario — what `check-report.mjs` keys on and
what `reportScenario.id()` builds in Go, so all three agree on what "the same
scenario" means. **Not the line number**: the two sides are read at different
commits, so any edit above a scenario moves its line while leaving the scenario
untouched. `line` is kept only for a "go to it" affordance.

A scenario in the report with no counterpart in the features gets its own
trailing group rather than being dropped — a removed scenario must not silently
vanish from a run's record.

## Why a line scanner and not `@cucumber/gherkin`

`parseFeature.ts` is deliberately the same scanner the run is checked with
(`skills/acceptance-run/scripts/check-report.mjs`). That file has no dependencies
because it runs in a validation pod; this one has none because a parser
generator is a lot of bytes to put in a console bundle for a grammar whose whole
surface here is `Feature` → `Rule` → `Scenario`.

The two MUST agree on identity, because the report joins on it: a scenario the
checker counts and this file does not would render as though the run had skipped
it, while the run's own contract gate stayed green. `parseFeature.test.ts` loads
`scanFeature` OUT OF the shipped checker — sliced from the file rather than
copied, because a copy keeps agreeing with itself after the original moves on —
and compares every feature file in the repo.

`Background:` is parsed although the authoring skill discourages it and no
generated file has ever carried one. Not to render it well, but so that a file
which does grow one cannot silently hang its steps off whatever scenario came
before.

## Tests

`AcceptanceView.test.tsx` covers the rendering in-package (jsdom via a per-file
`// @vitest-environment jsdom` pragma, matching `validation-view`);
`parseFeature.test.ts` pins the agreement with the checker; `report.test.ts`
covers the tolerant parse and `decidingStep`; `outcomes.test.ts` the vocabulary;
`tokenize.test.ts` proves the emphasis is lossless, since anything that failed to
reassemble would be a step the reader is quietly shown wrong; `filter.test.ts`
covers the three-way narrowing on its own, because a combination of filters is
where this sort of thing goes wrong and a component test is a poor place to find
out.
`vitest.config.ts` scopes `include` to `src/` because `build` compiles the tests
into `dist/` and the default glob would run those stale copies.
