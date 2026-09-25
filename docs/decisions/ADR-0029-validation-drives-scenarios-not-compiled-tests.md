# ADR-0029 — Validation drives scenarios, it does not compile tests

**Status:** Accepted · **Supersedes**
[ADR-0010](../../runners/remote-worker/design/decisions/ADR-0010-a-validation-report-is-an-accumulation.md)
(the report's shape) and the Playwright half of
[ADR-0012](ADR-0012-one-debian-runner-image-for-both-task-kinds.md) (what the image carries a browser
for).

The experiment this began as has concluded, and the compiled path is **removed**: the
`validation-criteria` skill, the JSON oracle it wrote, and the console viewer that rendered it. The
older ADRs describing that path are left standing rather than rewritten — they record why it was
built and why it was replaced, which is the reason to keep them.

## Context

The incumbent validation phase compiled each `method: e2e` criterion from
`specs/validation/validation-criteria.json` into a committed Playwright spec at
`tests/e2e/specs/<AC-ID>.spec.ts`, ran the suite, healed what failed, and reported. That seam — a
plan row joined to a spec file by an id — is the thing this removes.

A playground experiment measured the alternative on one app, four ways (two oracles × two execution
methods), and established the result that shaped this decision: the **oracle**, not the execution
method, is what moves the verdicts. The same app judged against Gherkin scenarios and against the JSON
criteria disagreed, while the same oracle executed two different ways agreed.

The economics were then measured on the platform itself, on two apps with both oracles minted blind in
one design turn and both arms grading the same deployed artifact. On the larger app the agent path was
**43% cheaper and 52% faster**, and — the result that matters more — its cost is **flat against oracle
size** where the compiled path's scales with it: the agent arm grew 0.4% while its oracle nearly
doubled, against 89% for the compiled arm, which writes a spec file per criterion. Coverage was equal
to within one criterion.

**Re-run economics remain unmeasured on both sides.** An earlier draft of this ADR asserted the agent
path was "4.5× more expensive by run eight" on the reasoning that a compiled suite is authored once and
replayed. That was an assumption presented as a finding, and it does not survive its own premise: the
compiled path heals and rewrites its specs between runs, so "authored once" describes neither arm.

## Decision

A validation run **drives the scenario text directly**. There is no generated test code and no
plan↔code seam, because the scenario IS the test.

- The oracle is `specs/validation/acceptance/<slug>.feature` — Gherkin, authored from the PRD
  alone by the `acceptance-criteria` skill. It sits **under** `specs/validation/` rather than
  beside it: that folder is the phase's, and it already holds the agent evaluation's own input
  (ADR-0035). Two sibling folders for one phase left a reader no rule for telling them apart.
  The oracle moved there from `specs/acceptance/` after the experiment settled, as a hard cut:
  a project repo written before that move has its features where nothing now looks, so it files
  no validation task and its version settles `skipped` until they are moved.
- The runner loads `acceptance-run` (always-on) and `agent-browser` (on demand) where it used to load
  `aep-validation` and `playwright-cli`.
- The report is `tests/acceptance/report.json`, keyed by scenario rather than by criterion id.
- `specs/validation/validation-criteria.json` is **gone**, along with the `validation-criteria`
  skill that wrote it. It was kept generated-but-unexecuted while the comparison still needed a
  second arm to isolate; once the measurements above settled the question, a second oracle nothing
  grades against is only a second answer for a reader to trip over.

### Four outcomes, not five

The report answers `passed` / `failed` / `blocked` / `unjudgeable` per scenario. The platform's six
run verdicts are unchanged; only their inputs are.

`blocked` is the one that earns its place. It says the agent could not carry out the `When` — the
control was `[disabled]` or absent — and it is NOT merged with `failed`, because the two are opposite
claims: one says the behaviour is wrong, the other says the behaviour was never reached. A compiled
suite cannot report this at all; a timed-out locator looks like a failure.

**`blocked` files no repair issue.** It lands on `partial`, and stops there. The agent cannot tell an
app that correctly refuses an action (a bought item has no edit control *because* bought items cannot
be edited) from one too broken to perform it. Auto-filing would send a coding run to add an affordance
the requirement never asked for — a repair loop that makes the product worse, confidently. A person
tells the two apart in seconds; the run reports and leaves it to them.

### One defect, one issue

A `failed` scenario becomes ordinary work: one issue per scenario, `bug` + `src/validation`, filed
into the milestone the attempt judged. One per scenario rather than one omnibus issue because the
no-progress rule compares working-set SIZES — repairing two of three failures has to read as progress,
and a single issue holding three could only be open or closed.

The dedupe key is the **scenario alone**. A scenario still failing when the next attempt runs already
has an open issue, so the mint resolves onto it and leaves that attempt's evidence there as a comment
instead of filing a second. The key carried the attempt as well at first, on the reasoning that a
scenario failing again must not be suppressed by the closed issue the last repair produced — which the
host's dedupe cannot do, because it only ever matches an issue whose state is OPEN. What the attempt
actually bought was a duplicate, and a second open issue for one defect grows exactly the set the
no-progress rule reads, so a defect that survived a repair registered as negative progress.

**The link between a repair issue and the run that found it points from the task, not to it.** The
validation task's close comment names the repair issues the attempt filed; the repair bodies reference
nothing. `Part of #N` in a repair body would aim a coding agent at the validation task, whose body is a
brief written for the validation agent — *drive every scenario*, *do not modify `specs/`* — so
following it would cost the reader context that is wrong for its job. Named from the task instead,
GitHub's own cross-reference puts a backlink in each repair issue's timeline, where a person sees it
and `gh issue view --comments` does not return it. A repair issue stays answerable from one read, which
is the property the whole body is built around.

### Evidence, not assertion

A pass is only worth the thing that could have said no. Every `Then` records the command that settled
it and that command's exit code, and `observed` is **required wherever the exit code is not the
verdict**: a nonzero exit, a step with no command at all, or a value-returning command like
`get count`, which exits 0 because the command *ran* while the agent did the judging.

This is the single most important property of the approach and the one most easily lost. It is
checked by a script the run invokes — deliberately not yet by the platform, which is an open question
this decision does not close.

### A failure carries what the system was doing

The trace is not the only evidence worth having, and a compiled suite is structurally unable to
produce the rest of it. A failing scenario records an `evidence` block captured **at the moment it
failed, while the page is still open**: the requests around the deciding step, any console errors, and
the page as the run saw it.

`network` is the half that earns its place. A request that left and came back `201` with the list
unchanged is a rendering defect; no request at all is a wiring defect. They are fixed in different
files, and nothing else in the report separates them — the step trace reads identically for both. An
EMPTY request list is therefore an answer, not a blank, and the checker distinguishes an empty array
from an absent key.

The repair issue renders the whole trace and the network and console lines. The **snapshot stays in
the report** rather than the body: it is the one unbounded item, and after the pull request merges
`tests/acceptance/report.json` is a file the coding agent already has checked out, so putting it in
the body buys reachability nothing.

Two things the run deliberately does **not** record. No screenshot — for an agent reader a textual
accessibility snapshot is strictly more informative, and a binary needs somewhere to live. And **no
root-cause hypothesis**: the validation agent never reads the source, so a causal claim from it is a
guess about internals it has not seen, produced by the one party whose whole credibility rests on
reporting only what a command settled. The repair agent, which is about to open the file, derives a
better one in a single read. What the live run uniquely has is *observational* — `POST /items → 201`
and the list still showing one item — and that is what is kept.

The gate is hard (`exit 2`) with one escape: an explicit `notCaptured` naming the reason. The escape
is not softness. The check runs after the page is gone, so an agent that did not capture can satisfy a
gate with no escape only by re-driving the scenario or by inventing a plausible request — and a report
that says something nothing checked is worse than one that states a gap.

### Isolation is functional, not a reset

The app under test is deployed and keeps its data: there is no process to restart and no database to
truncate. Scenarios isolate by **creating the thing they assert about** — a round, a board, a list —
so "the list is empty" is true by construction. Where a product has no such container, a scenario
asserts on the *change* instead of the total.

This is Meszaros's Database Partitioning Scheme and Farley's functional isolation; the fallback is his
Delta Assertion. The incumbent has **no isolation guidance at all**, so this is a gap the new path
names rather than one it introduces.

## Consequences

- **The console renders the report raw.** *Superseded — the shaped view landed once there were real
  reports to design against; see `apps/console/design/decisions/ADR-0031-the-report-annotates-the-specification.md`.* The reasoning stands as
  the reason it waited: the acceptance run answers per scenario and the criteria are a different
  decomposition of the same requirement, so there is no id to join them on, and passing the report
  through the criteria-joining path would have rendered `Not validated` on every row — a verdict,
  where the truth is that the report does not speak about criteria. The shaped view joins the report
  to the FEATURE FILES instead, on feature + rule + scenario, which is the identity this report's own
  checker already keys on.
- **Real-time progress is dark.** The matchers that drove it keyed on Playwright file writes and spec
  names, so against an agent driving a browser they matched nothing. Deleted rather than rewritten;
  the redesign is its own piece of work.
- **The image kept `@playwright/test`** — not as a test runner, but as the delivery mechanism for the
  chromium `agent-browser` launches. The second chromium that `playwright-cli` pinned is gone, and so
  is Playwright itself: the browser is now a Debian package
  ([ADR-0016](../../runners/remote-worker/design/decisions/ADR-0016-the-browser-comes-from-debian.md)).
- **The phase is still called Validation.** Validation is the objective, acceptance testing the
  activity that serves it, and an acceptance criterion the unit it grades — different axes, all three
  correct at once (`docs/glossary.md`). Nothing user-facing is renamed.
