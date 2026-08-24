# ADR-0011 — A GitHub milestone is the unit of execution

**Status:** Accepted · **Refined by:** [ADR-0020](ADR-0020-a-run-species-is-a-workflow.md) (a run
species is its own workflow; the milestone is still the unit of execution)

Delivery used to execute a spec version **task by task**. The planner wrote each
task issue with an embedded `aep:task/v1` machine block naming its component and
its `dependsOn` edges; two independent layers then re-derived a dependency
order from that block — an execution funnel with a `depsGate`, and a Temporal
`DevFlowWorkflow` fanning out one `TaskFlowWorkflow` per task. One coding-agent
pod was dispatched per issue, and a `v<N>` tag keyed everything: the workflow
ids, an `aep:spec/<tag>` label on every issue, the build reads, the console.
Progress was a ten-value derived-status algebra computed from execution rows.

Three things were wrong with it. The platform **parsed issue bodies it had
written itself**, so the issue was a database record wearing a prose costume and
a human editing it could corrupt the pipeline. The **dependency order was
computed twice**, in two places that could disagree, from a plan an LLM authored
and could not be held to. And **one pod per issue** meant N cold starts, N
workspaces, N pull requests, and no agent ever saw enough of the increment to
sequence its own work.

## Decision

**One spec version is one GitHub milestone, and supervised runs work that
milestone until it settles.**

A milestone is worked by ONE live run at a time, of one of three SPECIES — `dev`
delivers the version, `task` works a defect inside a version already delivered,
and `validation` judges a deployed version against its acceptance criteria. Each
species is its own top-level workflow ([ADR-0020][adr20]); every predicate in the
platform is written on the run's `kind`.

- **The milestone is the version.** It is titled after the `v<N>` tag, and its
  **number** is the platform key everywhere. `aep:spec/<tag>` is dead; a
  `?tag=` query resolves to a milestone number through the platform's own run
  rows, never by matching titles against GitHub. The milestone is also the
  version's **ledger** — fix, conflict, validation and incident issues join it
  over its life.
- **Nothing platform-side parses an issue body.** Bodies are prose the platform
  writes *for the agent*: what to build, its App Path, and "Depends on #N" lines
  the **agent** honours. Every routable fact is a LABEL, on two axes: `aep`
  **arms** an issue (and is the adoption trigger), and one KIND says what it is
  — `development`, `bug`, `conflict`, `validation`, `provision`. Every routing
  predicate is then a positive membership test on the kind rather than a
  subtraction of exclusions. Re-plan dedupes on the title slug against the
  milestone's own issues, which makes reconcile additive-only.
- **One agent per cycle, not per issue.** The dispatch prompt is a milestone
  reference; the runner discovers its own working set, orders it, fans out to
  Task subagents where the work is genuinely independent, and ships **one
  agent-validated pull request per cycle** carrying `Resolves #N` for every
  issue it finished. Both task kinds run on one Debian runner image. A
  **validation** cycle is the exception on both counts: it is anchored to one
  issue rather than a working set, and its pull request references that issue
  with the deliberately NON-closing `Validates #N`, because the platform owns
  the validation task's lifecycle ([ADR-0020][adr20]).
- **The event plane detects; the supervisor decides.** Webhooks and OpenChoreo
  polls auto-merge the pull request, fan a merge out to a build per changed
  component by path-diff, mint fix / conflict / red-main issues, and *signal*.
  A slim per-run Temporal workflow — `<kind>-<org>-<project>-<milestoneNumber>`,
  one per species — owns the wait state, the cycle loop, the budgets and settle,
  and imports no GitHub client. That dependency direction is a compiled package
  boundary, not a convention. The **kind prefix** is load-bearing: ids are reused
  after a terminal run, so without it a stale signal aimed at a settled dev run
  would be delivered to the validation run that later claimed the same id. The
  run ROW is the routing table — the event plane resolves a row before it signals
  anything, and the row's kind gives the prefix.
- **A signal is a wake-up, never evidence.** The supervisor re-reads ground
  truth before acting, so a lost webhook costs latency rather than correctness —
  which is what lets the wait state be unbounded with cancel as its only expiry.
- **Every budget names exactly one failure class**, so a terminal reason is an
  explanation: `redispatch-budget`, `build-retrigger-budget`, `deploy-budget`,
  `fix-chain-budget`, `conflict-budget`, `no-progress`, `cycle-ceiling`,
  `validation-failed`, `validation-unreported`, `plan-failed`. The reason is read
  together with the run's SPECIES, which is what keeps it honest across the
  split: `redispatch-budget` on a dev run means the delivery agent died, while on
  a validation run it means the judge did and the version is still delivered.
  The two validating reasons belong to a validation run alone.
- **A verdict is a fact about a VERSION, not a step in delivering one.** A dev
  run settles at deployed-green having minted the version's validation task, and
  never validates; the validation run started off that task produces the answer.
  The newest *validating* run on a milestone owns that version's verdict, so a
  dev run's empty verdict means "not judged yet" rather than "judged and fine".
  Why the two cannot share one workflow — different lifetimes, different failure
  classes — is [ADR-0020][adr20].
- **Re-planning is supersede-on-next-build.** The next build empties the previous
  version's milestone and then closes it, and plans `v<N+1>` fresh from the new
  spec. A plan is replaced by a plan, so its `development` issues and its gates are
  closed with a `Superseded by v<N+1>` comment; a DEFECT is not superseded by
  anything, so its open `bug` issues are moved into the new milestone, which is why
  that milestone is minted first. Cancel abandons an increment; the only way
  forward is the next build.

The mechanism — package shape, ports, and the full invariant list — is
documented where it is enforced: [`internal/delivery/README.md`][delivery] (L2)
under the README ladder ([ADR-0008][adr8]).

## The read model this forces

Loop position is **never a stored phase enum**. A fix or conflict cycle
re-enters an earlier phase, so a flat enum lies mid-loop; position renders from
the latest **cycle record**, joined live. Per-component build and deploy status
is derived from OpenChoreo on read, never stored.

Reads split into **two planes, priced apart**:

| Plane | Source | Cost |
|---|---|---|
| Run rows + cycle records (`/builds`, `/builds/{tag}/runs`, `/runs/{id}/progress`) | Postgres, fed by webhooks | free to poll at 5s |
| Issues (`/tasks?tag=`) | GitHub, live | polled only while a run is live, plus one fetch at settle |

Consequently **`ProjectStatus` carries no task tally**. Its only honest source
is the version's milestone on GitHub and that endpoint is polled at 5s, so the
console renders counts from the issue list it already holds, on the surface that
already pays for it. For the same reason the run state — not a task's status —
is the console's single liveness driver.

The console rendering that follows from this is a console decision:
[`apps/console/design/decisions/ADR-0013-version-run-surface.md`][console-adr].

## Consequences

**Accepted costs.**

- **A whole cycle is the unit of retry.** When an agent dies, the cycle is
  re-dispatched, not one issue — bounded at 2 dispatches per cycle.
- **No pre-merge build verification.** An in-pod container build gate was
  designed and prototyped, then dropped: it required rootless podman with
  `--isolation=chroot` and *both* seccomp and AppArmor unconfined, inside the
  pod that carries the Anthropic key and the GitHub token. The post-merge
  OpenChoreo build is the only build verification, so a broken Dockerfile
  reaches `main` and costs one of the run's two fix cycles to catch. The agent's
  **compile-level** verification (`go build` / `tsc --noEmit`) stays — it needs
  no container runtime.
- **The agent's plan is not auditable as data.** Ordering is prose an agent
  interprets; there is no dependency graph to inspect or to test against.
- **No milestone backfill.** Cutover was drain-and-abandon: versions built
  before it show an empty issue list, and incident adoption on a project last
  deployed pre-cutover errors clearly until the first post-cutover build heals
  it.

**What we no longer maintain.** Two dependency layers, `DevFlowWorkflow` /
`TaskFlowWorkflow` / `depsGate`, the executions store as an agent-work record,
the ten-value derived-status algebra, and the machine-block encoding —
`contracts/taskmeta`'s `block.go`, `derive.go` and `labels.go`. `derivedStatus`
survives as two values, `pending` (the issue is open) and `merged` (closed),
both deliberately members of the retired vocabulary because the console consumes
the field untyped. `taskmeta/execution.go` survives too: provisioning gates still
keep execution rows, and `TestTaskmetaIsPure` still guards it.

**A lesson worth keeping.** The milestone dispatch predicate shipped believing
GitHub GraphQL's `labels:` argument was an AND. It is a **union** — the
AND-semantics is real but belongs to the REST `?labels=a,b` parameter. Every
`gittest` fake modelled the same misconception, so the whole tier stayed green
while the live working set read zero and runs settled versions nobody had built.
A fake answers whatever the test authored: it is evidence of parsing, never of
semantics. Where a query's *meaning* is load-bearing, confirm it against the
real host once and record the answer next to the query.

The mechanism a cycle is dispatched by — one ephemeral `coding-agent` job
Component per cycle — is documented in
[`codingagent/design/oc-job-dispatch.md`](../../services/aep-api/internal/delivery/codingagent/design/oc-job-dispatch.md).

[adr20]: ADR-0020-a-run-species-is-a-workflow.md
[delivery]: ../../services/aep-api/internal/delivery/README.md
[adr8]: ADR-0008-architecture-in-readme-ladder.md
[console-adr]: ../../apps/console/design/decisions/ADR-0013-version-run-surface.md
