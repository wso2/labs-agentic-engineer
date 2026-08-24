# ADR-0020 — A run species is a workflow, not a branch

**Status:** Accepted · **Refines:** [ADR-0011](ADR-0011-milestone-is-the-unit-of-execution.md) (the
milestone is still the unit of execution; what changes is how many supervisors work one)

## Context

ADR-0011 put ONE supervised run on a milestone and worked it until it settled. Judging the result was
that run's last cycle: at deployed-green the supervisor minted the version's validation task,
dispatched an agent at it, read the committed report back as the run's verdict, and — on a failure it
had attempts left for — filed a repair issue per criterion and went round again.

It held together while a verdict was a step in delivering a version. It stopped holding as soon as the
verdict became a fact about a version that had *already* shipped:

- **The two answers have different lifetimes.** "Is the increment built" is answered in minutes and
  then never again. "Does the deployed system hold" is asked at deployed-green, asked again after a
  `src/validation` fix months later, and asked by a human clicking revalidate on a version three
  releases old. A workflow that owns both cannot settle: the run that delivered `v3` is the only thing
  holding `v3`'s verdict, so it either stays alive indefinitely or the verdict is orphaned.
- **The failure classes are not interchangeable.** A validation agent that dies through its whole
  re-dispatch budget failed the entire version — `redispatch-budget` on the dev run — even though every
  line of code had merged, built and deployed. The version was delivered; nobody had looked at it. One
  run row cannot say both.
- **The repair loop ran inside the delivery loop.** A failed verdict filed bugs into the working set
  the same run was polling, so the boundary's own rules (no-progress, the cycle ceiling, the fix chain)
  were being asked to bound work minted by a stage that sat outside them. `RunValidates` existed
  precisely to tell the loop which of two shapes it was in.
- **The revalidate endpoint had to fake a run.** It started a run whose working set was empty *by
  design* and whose first act was to skip the loop — the loop it nominally shared — and enter at
  validation. Every predicate in the boundary carried an exception for it.

There are three species of run, and `kind` (`dev` / `task` / `validation`) already named them. They
were three branch sets inside one workflow.

## Decision

**Each run species is its own top-level Temporal workflow.**

```text
DevRunWorkflow          gates → plan → cycle loop → mint the validation task → settle
TaskRunWorkflow         cycle loop → settle
ValidationRunWorkflow   adopt-or-mint the task → agent stage → verdict → repair issues → close → settle
```

A **dev run settles at deployed-green having minted the version's validation task, and never
validates.** A **validation run**, started by the reconcile sweep because an open `validation`-kind
issue exists, produces the verdict. `RunValidates` now answers `validation` and nothing else, so the
newest *validating* run on a milestone owns that version's answer and a dev run's empty verdict means
"not judged yet" rather than "judged and fine".

### They are independent executions, not children of a parent

The obvious alternative is a milestone-level parent workflow with three child executions. It is
rejected, and the reason is the lifetime problem above stated precisely: **a parent would have to
outlive what it supervises.** A build settles; validation may start much later, or after a
`src/validation` fix, or never — a project with no acceptance oracle never gets one. A parent waiting
on a child that may never be started is a workflow with no terminating condition, which is the exact
shape that made validation-inside-delivery unsettleable. Child workflows would also make cancel and
signal delivery a two-hop question, and put a run's budgets one level away from the run that spends
them.

Independence costs nothing in routing because **the run ROW is the routing table.** The event plane
already resolves a run row before it signals anything, and the row's `kind` gives both the workflow
type and the workflow id's prefix (`delivery.RoutableRunKind` → `RunWorkflowName` /
`MilestoneRunWorkflowID`).
There is no lookup table to keep, and no parent to ask.

### The kind prefix on the workflow id is load-bearing

Ids are reused under `WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE`, because a milestone sees sequential
runs of one kind across its life. Three species sharing one grammar would therefore claim the SAME id
in turn — and a stale `pull_request` signal aimed at a settled dev run would be delivered to the
validation run that claimed the id afterwards, which would then read the cycle facts of a merge that
was never its own. The grammar is `<kind>-<org>-<project>-<milestone>`; a row whose kind is empty is
addressed by the kind its ORIGIN implies, which is the only history that can produce one, and `dev` is
what a row carried before the column existed. A row that is neither — a valid kind, nor an empty kind
beside a known origin — is REFUSED by the start and by the signal alike, because its only available
reading is `dev`: the kind that takes the build mutex and plans a version, so guessing it starts a build
nobody asked for. Refusing to signal such a row loses nothing, since the same predicate means no
execution was ever started for it.

`AbandonRun` (project delete) consequently terminates **all three** ids. There is no row left to ask
which ever existed — the rows are purged in the same teardown — so a kind missed there leaves a
supervisor retrying its milestone poll forever against a repository that is gone, squatting on an id
any later same-named project's first run is then refused as `AlreadyStarted` on.

### One `Activities` struct, three `RegisterWorkflow` calls

Temporal registers an activity by its **reflected method name**. Two activity structs sharing any
method name panic the worker at Start — a boot-time crash whose stack names neither workflow — and
three structs carved out of one loop would share a great many. So the split is by FILE inside one
package, over one shared `loop` struct (it owns the signal channels, the budgets and the cycle state,
and every workflow wants all three), with one `Activities` struct and three workflows taking method
expressions off it. That is the only shape that cannot break that way, whatever gets added later.

Sub-packages were considered and rejected: `internal/arch` gives siblings a blanket import ban with no
layer concept, and second-level packages are unchecked in both directions — so sub-packages would be
*less* protected than files.

**Dev and task are the same loop with different bookends** — one `bookends{work, before, onEmpty}` value,
not two cycle loops that drift apart. Every rule that makes the loop safe is therefore one implementation:
a fix applied to a defect run cannot silently miss a release.

The `work` bookend is the WORKING SET, and it is the most consequential of the three because it decides
what a dispatch is spent on and what an empty milestone means. A dev run works `development` + `bug` +
`conflict`; a task run works `bug` + `conflict` and **never planned work**. A dev run owns the version and
holds the project's build mutex, so planned issues left open by a build that gave up must wait for another
build — not be continued by a run that never planned them, works the DEPLOYED version instead of the one
being built, and carries different budgets. Both counts ride ONE boundary poll, because the host returns
every population in the same GraphQL response.

### The validation workflow does not share the cycle loop at all

It has no working set to poll, and its pull request touches only `tests/` — so the path diff yields no
components and both the build and the deploy stage were already silent no-ops for it. It **skips them
outright**, which is the honest form of what was already true and removes two stages' worth of failure
modes from a run that could never reach them.

Two things span validation runs: the version's attempt allowance and the previous report's digest.
Neither is carried, because there is nothing to carry them in — each attempt is its own execution, so
the previous one's state is gone. Both are **derived from the ledger**: attempts is how many
`kind = validation` runs the milestone has, and the digest is the newest prior validation cycle's
`run_cycles.validation_digest`. Two consecutive identical digests prove the repair moved nothing and
stop the chain even though the allowance is not spent.

The digest is written by the **same activity as the verdict**, and must be: that cycle write is fenced
write-once on an empty verdict, so a digest recorded afterwards could never land on the cycle it
belongs to — and the next attempt would silently have nothing to compare against.

### The validation task's close is the platform's, and every ending performs it

The reconcile sweep starts a validation run BECAUSE an open `validation`-kind issue exists. So a run
that gave up and left the task open would be restarted within a tick, give up again, and keep doing
that forever, paying for two agent dispatches each time — and nothing outside the workflow can repair
a dead dispatch. **The task is therefore closed on every ending, verdict or no verdict**, including an
agent that died through its whole re-dispatch budget. What that leaves is a version that is deployed
and unjudged: honest, since no verdict is claimed, and one click from being asked again.

### `Validates #N` — and why not simply dropping `Closes #N`

Platform-owned close means the validation pull request must not carry a GitHub closing keyword: two
owners on one issue race on every attempt, and a reopen for the next attempt racing the host's own
close is indistinguishable from a human reopening it.

**Taken literally, dropping `Closes #N` breaks auto-merge.** `decideAutoMerge` requires a pull request
to reference at least one armed issue in the milestone, and the reference is parsed by `resolvesRefRE`,
which matches only GitHub's closing keywords. A validation pull request referencing nothing would be
treated as somebody else's work, would never merge, no report would ever be committed, and **every
validation would settle `unreported`** — a verdict about the software produced entirely by the
platform's own filter.

So the reference stays and becomes non-closing: **`Validates #N`**. `Validates` is not a GitHub closing
keyword, so the host closes nothing and the platform is the single owner. `resolves.go` gains a second
parse and `decideAutoMerge` admits it, **scoped to a reference to a `validation`-kind issue** — without
that scope it would become a general-purpose way to get a pull request merged while closing nothing,
and the working set would never empty. A coding pull request carrying only `Validates #N` is declined.

The two lists never merge. A coding pull request's `Resolves` list is also the durable record of what
that cycle finished (`RunCycle.Resolves`), so folding a reference that closed nothing into it would
claim work that is still open.

### The sweep becomes the trigger router, and reads issues

For each known milestone with no live run it routes on the TRIGGER PREDICATES themselves: an open armed
`validation`-kind issue starts a validation run, open task working-set work (`aep` + `bug`/`conflict`)
starts a task run, and anything else starts NOTHING. "Anything else" is a real population — a ledger
note, a `provision` gate, planned work a build gave up on — and offering a run for it dispatches an agent
whose working set is empty by construction, which parks and is re-offered every pass. A gate holds the
next DISPATCH, so with nothing behind it there is nothing to hold; `development` is dev-workflow's alone,
and only the build click may start a dev run because it carries the version mutex and the tag. It reads
the milestone's OPEN ISSUES (REST, no label filter) and decides in Go, because routing by kind is an
intersection GraphQL's union-valued `labels:` argument cannot count — the same shape, and the same
reason, as the auto-merge policy. One REST call
per known milestone per pass replaces one GraphQL call; the cycle-boundary poll keeps its counts,
because that read runs at every boundary and is the loop's hottest.

### A failed run must halt its own leftovers

The sweep's trigger is "open work of a species on a milestone with no live run", and a run that exhausts a
budget settles `failed` leaving that work OPEN — the milestone stays open too, because the way forward from
a failed increment is more work in the same version. The sweep cannot tell "given up on" from "not
started", so it would start a fresh run with a fresh budget on the same issues, within a tick, forever.
Every budget in the platform defeated at once, with a cloud bill for a symptom rather than a failing test.

So **every failed settle stamps `aep:halted`** and a comment naming the terminal reason on each working-set
issue the run could not finish — the recovery bugs it filed itself included, since those are the newest
things in the milestone and therefore the first a restarted run would pick up — and **the sweep skips
halted issues**. Two things clear the mark, and both are somebody deciding the work is worth another
attempt: a person removing the label, and the next build, which strips it from the bugs it carries forward.
That exclusion is a decision over issues the sweep ALREADY FETCHES, never a query filter:
"armed AND halted" is the same intersection the kind routing is, and its complement is a negative label
query the host cannot express at all. It therefore costs no round trip. The cycle-boundary poll is left
alone, because a halted issue inside a live run's milestone is a contradiction — the run that halted them
is terminal by construction.

The reach is the RUN's working set and nothing beside it, which is the second reason the working set is
per species: a dev run must not halt a bug a concurrent task run is working, and a task run must not halt
planned work it was never allowed to touch. A validation run halts nothing at all — its own work is the
task it closes on every ending, and the repair and conflict issues it leaves behind are deliberately a task
run's work, so halting them would break the repair chain rather than protect a budget.

### A cancel has per-species consequences, and one way back

Cancel is durable first and a signal second (the run row's `cancel_requested_at` is the evidence; the
signal is only the wake-up). What the split adds is what a cancel COSTS, and it is decided by kind for
the same reason the working set is.

**Closing the issues is the cancel, not bookkeeping for it.** The sweep starts a run over a milestone's
open WORK when no run is live on it, so a cancel that merely recorded itself would be undone
within a tick — the button would stop the run and then pay for its replacement a minute later. So a
cancelled settle comments, stamps `aep:cancelled` and CLOSES, through the event plane's
`CloseCancelledWork`: the same shape as the halt, reached from the other ending, so the supervisor still
writes no issue of its own.

| cancelling | closes | milestone | way forward |
|---|---|---|---|
| dev (a build) | everything the increment carried — working set AND gates — but NOT the version's validation task (a handle on software still deployed) and NOT the ledger (never the platform's to touch) | CLOSED | edit the spec, or build again |
| task (a bug fix) | the `bug` and `conflict` issues it was working | left OPEN | reopen the bugs, or file new ones |
| validation | the task it ADOPTED (already done on every ending) | left OPEN | trigger validation again |

**A dev cancel takes the gates; a halt does not, and the difference is the whole point of having two
markers.** A halted run may be retried in the same version, so its gates still name dependencies
somebody must resolve — closing them would erase the record of what the version was waiting on. A
cancelled build will not be retried in that increment at all.

**Nothing is reverted.** Merged commits stay on `main` and components a cycle already promoted keep
serving. Closing the milestone is a statement about the INCREMENT, never about what is deployed.

### The milestone closes on a green ending

One predicate for all three workflows (`delivery.SettleClosesTheMilestone`), because one `settle`
function serves all three and "succeeded closes it" is wrong for two of them.

| settling | milestone | why |
|---|---|---|
| dev, having filed the validation task | left OPEN | the version is deployed and UNJUDGED; the task it just filed is what judges it |
| dev, having filed none | CLOSED | no oracle, or a plan that minted nothing — nothing is coming |
| validation, succeeded | CLOSED | the version has its verdict, and every fatal verdict settles the run `failed` instead |
| task, succeeded | left OPEN | a defect fixed inside somebody else's version says nothing about that version |
| any kind, failed | left OPEN | the way forward is more work in the same version |
| dev, cancelled | CLOSED | the increment is abandoned (see the cancel table above) |

Closing at the dev run's HAND-OFF is not merely early — it breaks the hand-off. The validation agent
discovers its work with `gh issue list --milestone`, which resolves the milestone by title and sees only
OPEN milestones, so the task would be undiscoverable by the only agent meant to work it.

**Only issues OPEN at cancel time are marked**, which is the whole reason the marker exists rather than
"a rebuild reopens the milestone's issues". Work a cycle genuinely finished is already closed and stays
unmarked, so a rebuild cannot resurrect it and dispatch an agent at code that is merged and serving.

**A closed milestone still accepts issues, so the sweep also skips the milestone whole** while its
NEWEST run reads `cancelled`. Without it, one issue reopened inside an abandoned increment starts a task
run that builds and deploys a version nobody is shipping. The rule clears itself: a rebuild admits a new
row on the same milestone, so the newest run stops being the cancelled one — no flag to set and nothing
to clear, which is why it reads the newest run of any kind rather than hunting the history for a cancel.

**The way back is decided by the spec-save status alone.** There is no second "was it cancelled"
question anywhere:

- `approved` → a new tag. The ordinary build path: supersede the predecessor (which finds nothing to do,
  because the cancel already emptied and closed that milestone), mint the new milestone, plan it fresh.
- `unchanged` → the SAME tag, so the same milestone. Reopen it, reopen exactly the issues carrying
  `aep:cancelled` and CLEAR the label (it records one abandoned attempt, not a property of the issue),
  admit a run row, and start it with `Rebuild` set. Gates still run and dedupe onto the reopened ones;
  **`planTasks` is SKIPPED.**

Skipping the plan is not an optimisation. Plan dedupe is the title slug against the milestone's issues
in ANY state — which is what makes re-planning additive-only and a crash re-run a no-op — so a re-plan
after a cancel that closed everything would recognise every slug, mint NOTHING, and the loop would then
read the empty working set as "delivered" and settle a version it never built. Reopening is the only
path that restores the working set without breaking additive-only dedupe, and it costs no LLM turn.
`Rebuild` rides the REQUEST beside `Tag`, under the same replay rule: the zero value is the pre-existing
behaviour, so a re-offer from the sweep can never claim a milestone was refilled for it.

### A build is refused while validation is live

409, alongside the build mutex's own refusal. A delivery run merging and promoting while validation
asserts against the deployment would be judging a moving target — the verdict would name criteria true
of neither the old release nor the new one. A validation run deliberately sits outside the build mutex
(it re-judges a version that already shipped, so holding up the next build for its duration would be
wrong), which is why this refusal is an explicit read rather than an index. The way past it is to
cancel the validation, which is one click.

## Consequences

- **The repair chain closes without a human.** A failed verdict files one `src/validation` bug per failed
  criterion and closes the task; an ordinary task run works them and, when its working set drains, REOPENS
  the version's validation task — so the sweep starts another validation run and the same oracle judges the
  repair. That is the task run's own bookend, and the one conditional in the platform where a `src/*` source
  routes anything. `src/incident` and `src/user` leave the standing verdict alone: an incident is not priced
  like a release, and a verdict is a statement about a VERSION rather than a commit.
- **The cancel button is honest at every tick, not just the first.** A cancelled increment's issues are
  closed and its milestone is skipped whole, so no later pass restarts what the button stopped.
- **Terminal reasons stay honest per species.** `redispatch-budget` on a dev run means the delivery
  agent died; on a validation run it means the judge did, and the version is still delivered. The list
  of reasons is unchanged — the split needed no new failure class.
- **The two verdicts a version can hold are on different rows.** The dev run's verdict column stays
  empty (or `skipped`, when there is no oracle and nothing will ever judge it); the validation run's
  carries the answer. Readers take the newest *validating* run on the milestone.
- **`RunValidates` narrowed rather than disappeared.** It is still the one place "which rows can carry
  a verdict" is written, which is what stops a task run's `skipped` from making a genuinely passed
  version read as unvalidated.
- **A version with no acceptance oracle records `skipped` at delivery.** No validation task is filed,
  so nothing will ever judge it, and an empty verdict would read as "any moment now" forever.
- **In-flight runs do not survive the upgrade.** The workflow type and the id grammar both changed, so
  a run mid-loop across the deploy is neither signalled nor resumed. Same drain-not-migrate posture as
  [ADR-0019](ADR-0019-deploy-order-follows-the-hard-wiring-edges.md), for the same reason:
  `workflow.GetVersion` would mean keeping the fused loop alive for old histories.
- **The platform change and the skill change ship together.** `skills/aep-validation` writes
  `Validates #N`; the dispatch prompt and the validation issue's body say the same thing and say why.
  Shipping either half alone breaks every validation — one way leaves two owners on the task, the other
  leaves the pull request unmergeable.

## Not done, deliberately

**A validation run does not rebase its own pull request.** A conflict on a validation pull request
settles the run under `conflict-budget`; the conflict issue the event plane minted is ordinary work in
the milestone for a task run to pick up, but nothing in this workflow can rebase a branch, so there is
no second attempt to make. Fixing it properly means the conflict recovery chain becoming reachable from
the validation workflow, which is the cycle loop it deliberately does not share.

The mechanism — the file split, the shared `loop`, and the full invariant list — is documented where it
is enforced: [`internal/delivery/README.md`][delivery] (L2) under the README ladder
([ADR-0008][adr8]), and [`internal/delivery/run/doc.go`][rundoc] for the three workflows themselves.

[delivery]: ../../services/aep-api/internal/delivery/README.md
[adr8]: ADR-0008-architecture-in-readme-ladder.md
[rundoc]: ../../services/aep-api/internal/delivery/run/doc.go
