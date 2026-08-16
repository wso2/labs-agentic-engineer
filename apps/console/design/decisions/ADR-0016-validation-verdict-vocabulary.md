# ADR-0016: The validation vocabulary — nine states, one mapper, and marks that must be announced

Status: Accepted. Amends ADR-0013 (version-run-surface) §8, which said the
Validation page "re-keys to the run's verdict"; that remains true of the verdict
itself and is completed by §1 below.

## Context

`deploy.validation` has nine values and they render on five surfaces: the
overview's deploy line, the deployments rail's stage fact, its verdict banner,
the Validation page's header chip, and the verdict tile's headline. One mapper —
`projects/lib/pipeline.validationView` — owns the label and tone for all of them,
so that a state cannot read one way on the board and another on the page.

Three of these decisions were made, unmade, and made again, which is what earns
them an ADR rather than a comment. `0b5cb688` removed the `partial` asterisk in
good faith on the grounds that the deployments surfaces print the criteria counts
beside the verdict; the counts do carry the hedge *where they render*, and the
label also renders where they do not.

## Decisions

1. **What to render is a JOIN of two facts, never the verdict alone.**
   `validationState(deploy.validation, verdict)`: the lifecycle from the status
   read, the verdict from the run row. `RunValidation.verdict` is a column with
   no lifecycle in it, so a surface reading it alone announces a terminal failure
   over a version the platform is repairing. A lifecycle value is honoured only
   over `failed` and `unreported` — the two the loop repeats — so two independent
   polls cannot pair a stale `awaiting-fix` with the newer poll's green verdict.

2. **`partial` is `validated*`, green, with a spoken form.** The tone reports the
   OUTCOME and the mark carries the COVERAGE caveat, and separating them is the
   point: nothing about a partial run failed, so green is honest, while the bare
   word "validated" claims a result for criteria nobody checked. The label must
   not spell the hedge out — the mark is meant to be quiet, and every surface
   with room says it in full a line later (the tile's sentence, the banner's).

3. **`info` is reserved for work in flight, and this is structural.**
   `deploymentStory.TONE_STATE` maps `info` to the rail's `active` state, which
   `StageDot` draws as a hollow pulsing dot. A settled verdict toned `info` would
   pulse on the rail forever. `running` is the only `info` in the vocabulary.
   *This is why decision 2 needs a mark at all: the "done, with a note" colour is
   not available to a settled state.*

4. **A mark that carries meaning must be announced.** `validated*` and
   `validation?` say what they mean in punctuation no screen reader reads, so
   both collapse onto a neighbouring state's name. Each carries a `spoken` form,
   which is an ACCESSIBLE NAME and never a visible substitute — putting
   "validated, partially" on screen would be a third wording and would make the
   mark unreachable.

5. **The spoken form is wired where the label stands ALONE.** The Validation
   page's header chip has nothing beside it that explains the mark, so it passes
   `spokenLabel`. The tile headline and the rail fact each have prose one line
   below spelling the hedge out in full; a second accessible name there would say
   it twice.

6. **It is visually-hidden text, not `aria-label`.** A `Chip` with no `onClick`
   renders a plain div with no role, and an `aria-label` on a roleless element is
   ignored — so the accessible name has to come from content: the marked string
   is hidden from the accessibility tree and the spelled-out one takes its place.
   `StatusChip.spokenLabel` implements this for every consumer.

## Consequences

- **Do not remove the asterisk to make `partial` match `passed`.** They are
  different outcomes and the label is the only thing distinguishing them on the
  three surfaces that print no counts. If counts are wanted everywhere instead,
  that is a change to those surfaces, not to the vocabulary.
- Adding a state means adding a label, a tone, and — if the label hedges with a
  mark — a `spoken` form and a `spokenLabel` at every standalone render site.
- Tone is not free styling: it is read by `TONE_STATE`, so choosing `info`
  chooses the rail's pulsing dot, and choosing `success` is what makes
  `PromoteDialog` say "Validated in dev" rather than "Validation in dev".
- Promotion is unaffected by tone. `canPromote` gates on `BLOCKING_VALIDATION`,
  a set of validation VALUES, so recolouring a state never silently changes what
  can be promoted.

## Rejected

- **Amber for `partial`.** Its copy ends "please validate them manually", which
  is a call to action, and `inconclusive` is already amber. But amber would put a
  run where everything that ran passed in the same bucket as one being repaired,
  and with the mark restored it hedges twice.
- **Spelling the hedge into the label** ("partially validated"). Legible, and it
  needs no accessible-name plumbing — but it is a third wording competing with
  the prose that already says it, and it is wider than every neighbouring label.
- **A tenth value for "running after a failed attempt".** `deploy.validation` is
  genuinely `running` for both a first and a repeat attempt because the server
  cannot distinguish them either; what differs is the run's verdict, which
  decision 1 already joins in.
