# ADR-0031: The report annotates the specification; it does not replace it

Status: Accepted, **on the `vld-redesign` branch only**. Implements the console
half that [ADR-0029](../../../../docs/decisions/ADR-0029-validation-drives-scenarios-not-compiled-tests.md)
deferred — *"the console renders the report raw … a shaped view is real design
work, and the right time for it is after reading a real report."*

## Context

The Gherkin path landed with two placeholder surfaces, and both were worse than
unstyled.

A `.feature` file was neither `.md` nor one of the known structured types, so it
fell through `SpecView`'s branch tree to `CollabTextArea` — an **editable
monospace textarea** over a document nobody edits by hand, which is exactly the
dishonesty `CommittedFileView` was written to remove for every other file type.

The Validations page was worse: it rendered the *criteria* oracle, which this
branch deliberately never executes, with no chips on any row (`hasRun` false),
and dumped `report.json` into a `<pre>` underneath. The tally was hardcoded
`undefined`, with a comment explaining that criteria and scenarios share no id.

Two real reports now exist (p55, p56) plus the `shopping-list` playground pair,
so the deferral has expired.

## Decisions

1. **The feature files are the spine; the report is an overlay.** One component
   (`@aep/ui-acceptance-view`), used twice: the Spec pane passes `features`, the
   Validations page passes `features` and `report`. Rendering from the report
   alone would be simpler and would make DRIFT INVISIBLE — the features are read
   at the branch tip and the report at the merge commit of the attempt that wrote
   it, so a scenario authored since has no entry. That is the ordinary authoring
   loop and it has to show. Same shape `@aep/ui-validation-view` already proved
   for the criteria.

2. **A new package, not an extension of `validation-view`.** Different oracle,
   different report shape, and that package's `report.ts` is now legacy-only
   (kept for reports already merged into project repos). It also keeps the
   deletion story clean if the branch is reverted whole.

3. **The outcome words are the report's own, title-cased.** No mapping table, so
   no fifth vocabulary to keep in step with `report.go`, `check-report.mjs`, the
   skill and ADR-0029 — and an unrecognised word renders verbatim and neutral
   rather than falling through to a wrong label, which agrees with the Go ladder
   counting an unknown outcome as a gap. Tone and glyph still need a table: at
   four outcomes, ADR-0016's rule (two outlined chips must not differ by hue
   alone) now applies to every one of them.

4. **The tally needs no join.** The acceptance run reports one entry per
   scenario in the feature files, and its own checker fails the run otherwise,
   so the report is its own denominator — where criteria and scenarios shared no
   id. `useValidationEvidence` (the deployments board) reads no oracle for the
   same reason.

5. **One card with a filter toolbar over a scrolling list**, following a UI
   designer's reference widget. Its geometry is reproduced exactly; its colours
   go through the theme, because it is light-only and the console is not. Two
   things in it are ours rather than theirs: `Rule:` survives as a band (their
   sample data has no rule field, so they modelled generic Gherkin rather than
   deciding against our dialect, where the rule carries the `@story-N`), and the
   file path under each title does not (the lexicon forbids quoting a repo path,
   twice — the counts go there instead).

6. **The Spec rail carries ONE "Acceptance criteria" entry**, not one row per
   capability, so both surfaces show the same set through the same renderer. It
   is the first rail entry standing for N documents, which costs two small
   things: `followSelection` routes an acceptance path to the entry (its own
   contract is that follow-the-write can never land where a rail click would
   not have gone), and the row's plan status is folded for a set — it pulses
   while ANY capability is being written and is a ghost only when NOT ONE is
   committed, since two written and a third planned is a real entry.

   Reading N live documents needed `useYTextStrings`: the existing hook binds
   one `Y.Text` and hooks cannot be called in a loop. Its snapshot is cached
   because an uncached one is not a subtle bug but an immediate render loop.

7. **Nothing expands by default, and every row carries a chevron.** Which makes
   the one clamped line under a non-passed row load-bearing: it is the only thing
   on the page saying why. It is the DECIDING step's `observed` —
   `deciding()` from `validation/report.go`, reused rather than re-derived, so
   the console's summary and a repair issue quote the same step.

8. **`observed` outranks `command`.** The command is clamped to two lines of 11px
   mono; `observed` gets body type and is never clamped. A real command runs to
   400 characters of `--fn` predicate and is often not a command at all (`n/a`,
   `(already signed in from prior scenario)`), while a blocked `observed` runs
   past 400 characters and is the only thing that lets a person settle "refuses
   correctly" against "broken". It is also why `blocked` files no repair issue.

9. **Per-scenario live progress is not ported; it is retired.** The criterion
   rows' live words (`Exploring…`, `Authoring…`, `Healing…`) were fed by
   `work_item` events from Playwright-shaped matchers ADR-0029 deleted, so
   `useValidationLive` folded nothing and `validationLiveLine` returned `""` on
   every run. Both are deleted rather than re-derived for scenarios: there is no
   source yet, and a vocabulary with nothing behind it is worse than silence. The
   agent's own posted status line survives and was always the better evidence.

## Consequences

- **`@negative` is a tag pill, not a glyph.** The pill says "negative" in
  readable text and doubles as a filter, where a glyph could only be looked at.
  All-happy-path is the commonest defect in a generated spec, and a reader can
  select for it in one click.
- **The view says nothing about the run itself.** No tally, no commit, no
  deployed URL, no isolation statement — the run's identity belongs to the page
  that owns the run, and the tally in particular was a rule this package broke
  and then restored (`validation-view/design/README.md`: counts belong with the
  verdict that explains them). The isolation statement is the one with a cost,
  and the split is deliberate: the run is still HELD to it — `check-report.mjs`
  fails a report that omits it — the reader is simply not shown it. The contract
  enforces; the page stays quiet.
- **The refusal mark reads the inherited property, not the raw tag.**
  `@negative` inherits `Feature → Rule → Scenario`, so a scenario under a
  prohibition rule has the property without carrying the tag — 22 of the repo's
  77 refusals. Reading the tag list showed those nothing, on the one mark whose
  job is "are the refusals covered". For the same reason a rule and a feature
  drop `@negative` from their own lists: every scenario beneath them shows it.
- **Amber means a person has to look, and it covers `blocked` AND
  `unjudgeable`.** They were split once on the claim that blocked was the only
  row with a call to action; the code disagrees three times over —
  `FailedScenarios` returns `failed` alone so neither files a repair issue, the
  verdict ladder pairs them in a single arm, and the partial sentence counts
  them together. A glyph tells them apart, which is what glyphs are for.
  `No result` stays neutral, and that distinction is real: a scenario authored
  since the run is the ordinary loop, not something to act on.
- **`@negative` takes `info`, and never amber.** The brand accent said "the
  product's own thing" about what is really an informational property of a
  scenario; amber would say a reader has to act on ordinary spec.
- **The outcome pill keeps a glyph the reference design does not have.**
  ADR-0016's rule — a mark so the set is told apart at a glance rather than read
  one at a time — is not something a restyle gets to drop, and four outcomes make
  it bite harder than the two it was written for.
- **The Spec pane's `.feature` files are read-only**, via `isStructuredFile`.
  That predicate is the whole fix for the editable-textarea bug.
- **Per-capability navigation left the rail.** You can no longer jump straight to
  a capability from the sidebar; the pane's search and its per-capability counts
  replace it. Better at the job, but a real trade rather than a free win.
- **The two oracles sit side by side** in the Spec rail's Validation section,
  because the branch still generates both as comparison arms. Left visible;
  hiding a generated artifact from its author is a separate call.
