# delivery — Delivery Pipeline

> **L2 · a domain.** Part of the [aep-api architecture](../../README.md).

Take a versioned Spec end-to-end: cut the version, plan its Tasks into a GitHub MILESTONE, and run one
supervised loop that dispatches the coding agent at that milestone until it settles — merging, building,
deploying and validating along the way. **Single write-authority over the milestone-run store and the one
Temporal workflow that drives it.**

```mermaid
flowchart LR
  API(["/api/v1"]) --> HTTP
  INT(["/internal/v1"]) -.-> VAL
  GH[["GitHub webhooks"]] --> EVENT
  subgraph delivery
    HTTP["httpapi — build · task · execution · runread handlers"]
    subgraph ROOT["root (shared kernel — types + ports + Temporal infra, no gorm)"]
      K1["Runtime · TaskStreamHub · ProvisionInput"]
      K2["milestone model — labels · run signals · StartRunRequest · RunStatus · workflow id"]
      K3["read DTOs — TaskView · ExecutionView · Lineage · TaskDetail"]
      K4["merge→builds contract — DiffComponents · BuildRunName · BuildTerminalObserver · MilestoneDispatcher"]
    end
    BUILD["build (buildpipe)"] --> ROOT
    TASK["task (taskflow)"] --> ROOT
    EXEC["execution — the executions READ surface + task-log stream"] --> ROOT
    EVENT["eventcore — merge policy · build fan-out · issue minting · sweep"] --> ROOT
    RUN["run — the milestone run supervisor (one Temporal workflow + its worker)"] --> ROOT
    RREAD["runread — the run read surface: version runs · progress SSE · cancel"] --> ROOT
    CODE["codingagent"] --> ROOT
    VAL["validation — S2S context/credentials · report verdict"] --> ROOT
    HTTP --> BUILD & TASK & EXEC & RREAD
    RREAD -.->|RunCanceller| RUN
    CODE -.->|CycleLogReader| RREAD
    CODE -.->|BuildTerminalObserver| EVENT
    CODE -.->|MilestoneDispatcher| RUN
    EVENT -.->|RunSignaler · RunStarter| RUN
    BUILD -.->|RunStarter| RUN
    BUILD -.->|SpecPlanner| TASK
    RUN -.->|ValidationCoordinator| VAL
  end
  EXEC --> EXECS[("executions")]
  EVENT --> RUNS[("milestone_runs · run_cycles")]
  RREAD --> RUNS
  BUILD --> RUNS
  TASK --> RUNS
  RUN --> RUNS
  RUN --> TMPRL[["Temporal"]]
  BUILD -->|SpecTagger · SaveSpec| SPEC[[spec]]
  BUILD -->|repo full-name · milestones · supersede| SC[[sourcecontrol]]
  BUILD -->|GateResolver| DEP[[dependencies/provisioning]]
  EVENT -->|issues · merges · builds · component CRs| SC
  TASK -->|milestone membership| SC
  CODE -->|org keys · git tokens| SEC[[platform/secrets]]
  EXEC -->|ExecutionReader| OPS[[ops]]
```

## Internal shape — kernel-root + feature sub-packages

Delivery is **not** the flat-root-of-services layout spec/organization/ops use, and **not** per-op slices.
Its absorbed features are densely cross-coupled AND carry a load-bearing split — the GitHub-facing Task
surface must never reach the dispatcher — which the ordinary rules (`root ⊥ slice`, `slice ⊥ sibling`)
cannot satisfy flat. The resolution: **anything referenced across a feature boundary is a TYPE or PORT
that lives in the ROOT; the feature logic that uses it lives in a sub-package importing only the root.**
`task` and `run` are then peer sub-packages that never import each other (`TestTaskRunSplit` re-asserts
it), and every former feature→feature edge becomes a legal slice→root type reference.

| Sub-package | Owns | Reaches the root for |
|---|---|---|
| `build` (buildpipe) | the whole-spec gate + `v<N>` tag cut, **the milestone plan path** (supersede the previous version, mint `v<N>`'s milestone, admit the run row, then plan its Tasks and mint its gates), the version ledger, dep-drawer preflight | `MilestoneRun`/`StartRunRequest`, and the planner via `SpecPlanner` |
| `task` (taskflow) | the GitHub-native Task READ surface (list/get, scoped to a version by milestone membership) + the plan turn, which mints one **prose** issue per Task **into the version's milestone**, assigned at creation; plus the SRE/RCA handoff's adoption leg | the read DTOs, the milestone label vocabulary, and the run rows (via `MilestoneResolver`) |
| `execution` | the executions READ surface: the per-Task progress endpoint, the task-log SSE stream, `OpsExecutionReader`. It writes nothing and dispatches nothing — the only execution rows left are the provisioning gates' | `TaskStreamHub`, the executions kernel |
| `eventcore` | the event plane of the milestone-run loop: the auto-merge policy seam, the merged-PR path-diff build fan-out + per-`(component, SHA)` re-trigger budget, fix/conflict/red-main issue minting, milestone-matched predicate re-evaluation, adoption, the reconcile sweep, and the build sweep that observes those builds reaching terminal | the milestone model (labels, `MilestoneRun`/`RunCycle`, run signals), `DiffComponents`/`BuildRunName` and `BuildTerminalObserver`; **no Temporal** — it reaches the supervisor only through the `RunSignaler`/`RunStarter` ports |
| `run` | the milestone run SUPERVISOR: the wait state + dispatch predicate, the cycle loop, the four budgets + no-progress + ceiling, the validation cycle, settle, and cancel. Plus the `Supervisor` handle the event plane and the build click signal and start runs through | `Runtime`, the milestone model, `RunStatus`/`MilestoneRunWorkflowID`, `MilestoneDispatch`, `DiffComponents`/`BuildRunNamePrefix`; **no GitHub client, no gorm** |
| `runread` | the run READ surface: a version's runs + their cycles, ONE SSE stream stitching the per-cycle agent logs, and the two writes beside them — cancel, and revalidate. Owns no state and decides nothing: both writes resolve their target through the org-scoped read, then hand off | the run/cycle entities and `IsTerminalRunState`; reaches the pod log through `CycleLogReader` (OC API while the Component lives, observer archive while retained), the supervisor through `RunCanceller` and the event plane through `Revalidator`, so it drags in neither a cluster client, a workflow engine nor GitHub |
| `codingagent` | the CodingExecutor (ONE dispatch entry point: dispatch a run cycle as an ephemeral OpenChoreo `coding-agent` job Component), the build-auth retry, the pod-truth watcher, retention/LRU and the cancel-time delete. Design: [`codingagent/design/oc-job-dispatch.md`](codingagent/design/oc-job-dispatch.md) | `MilestoneDispatch`/`MilestoneDispatcher`, `TaskStreamHub`, `BuildTerminalObserver` |
| `validation` | the two S2S validation runner callbacks (context / test-credentials), the per-version validation issue, and the report → verdict rule | — (no cross-edges; least entangled) |
| `httpapi` | the aggregator: embeds build/task/execution/runread handlers; **holds `Deps`** (see below) | imports the sub-packages (the exempt aggregator) |

**`Deps` lives in `httpapi`, not the root.** Every other domain keeps its `Deps` in the domain root, but
delivery's services live in sub-packages the root may not import (`root ⊥ slice`). The `httpapi` aggregator
is the one package allowed to name them, so `httpapi.Deps` + `httpapi.New` is where composition sits.

## Ports
| Port | Dir | Peer · contract |
|---|---|---|
| `Adopter` (`AdoptIssue`) | needs | `task` → the event plane. The handoff's dispatch leg: file a bare issue under the milestone of the deployed version (or, with none deployed yet, the spec build in flight), mark it agent work, and start an incident run over it |
| `MilestoneResolver` (`MilestoneNumberForTag`) | needs | `task` → the root run repository. A `?tag=` query is milestone membership, resolved through the platform's own rows |
| `SpecTagger` (`*spec.SpecSaveResult`) · `SpecCollector` · `AuthDeriver` | needs | `spec` — the whole-spec gate + tag cut + design reads |
| `RepoLookup` (`owner/name`) | needs | `sourcecontrol` — repo full-name resolution |
| org-credential reads | needs | `platform/secrets` / P3a org repositories — the GitHub + publisher halves of a coding-agent run's secrets |
| `CodingKeyResolver` (`organization.SecretRefTriplet`) | needs | `organization` — WHICH Anthropic credential this org's coding runs bill (its coding-agent override, else its default key) and the env var it must be mounted as. A domain decision, so the port exposes no way to ask "is there an override?"; dispatch only mounts what it is handed. See ADR-0016 |
| `ExecutionReader` (`ops.ExecutionFact`) | offers | `ops` — latest-execution-per-kind correlation (`execution.OpsExecutionReader`, P6-retired the app bridge) |
| `BuildTerminalObserver` (root) | offers | the OpenChoreo watcher → the event plane: a settled build reported outwards, so watcher and event plane stay peer sub-packages |
| `MilestoneDispatcher` (root, over `MilestoneDispatch`) | offers | the coding agent → the supervisor: launch one agent run at a milestone and answer with its Job ref. The dispatch prompt is a milestone reference; the runner discovers its own working set. Satisfied by `*codingagent.CodingExecutor`, which writes no execution row — the cycle record is the supervisor's bookkeeping |
| `ComponentEnsurer` | needs | `eventcore` → the projects component service + the runtime-config emitter. Provision a component's OpenChoreo CR immediately before its first build; see the invariant below |
| `RunReader` · `CycleReader` · `CycleLogReader` · `RunCanceller` · `Revalidator` | needs | `runread` → the root run/cycle repositories, `codingagent`'s cycle-log reader (the pod's log through the OC API while it lives, the observer's archive while the Component is retained), `*run.Supervisor` and the event plane. Four reads and two writes, which is the whole dependency surface of the read model. `Revalidator` is a port for the same reason `RunCanceller` is: deciding a revalidation needs GitHub (is there open work?) and the project repo (is there an oracle?), and this surface must stay free-to-poll |
| `RunSignaler` · `RunStarter` | needs | `eventcore` and `build` → the run supervisor. Signal a run, start one. Interfaces, which is what keeps both the event plane and the build click free of a workflow engine; both are declared over the root `StartRunRequest`, and `*run.Supervisor` satisfies both |
| `RunStore` · `CycleStore` · `MilestoneReader` · `PRReader` · `DesignReader` · `BuildReader` · `ValidationCoordinator` | needs | `run` → the root repositories, `sourcecontrol`, the design reader, `clients/openchoreo` and `delivery/validation`. Every I/O the loop performs, named once; `BuildReader` is read-ONLY because the supervisor never triggers a build |
| `MilestoneClient` (mint · list a milestone's issues · close issue · close milestone) | needs | `build` → `sourcecontrol`. The plan path's whole GitHub surface: create `v<N>` idempotently, and supersede `v<N-1>` |
| `MilestoneRunStore` (active-run read · admit · settle · list) | needs | `build` → the root run repository. The 409 pre-check and the admission that arms the spec-run mutex |
| `SpecPlanner` (`PlanIntoMilestone`) | needs | `build` → `task`. The planning turn, reached through the root exactly as `TaskReader` is, so `build` names no sibling |
| `GateResolver` (author dependencies + mint gates into a milestone) | needs | `build` → `dependencies/provisioning`. Gates are dispatch holds, never agent work |
| `BuildTrigger` (stage the org clone credential · trigger at commit · list a component's runs) | needs | `clients/openchoreo` — the fan-out, and the run list the re-trigger budget is derived from. Staging is its own verb because the credential is per-ORG while a trigger is per-component: a caller building N components stages once and reuses the reference |
| `IssueClient` (mint · milestone membership · milestone counts · assign) · `PRReader` · `PRMerger` | needs | `sourcecontrol` — every GitHub write the event plane makes, on the org's own credential |
| `ValidationContext` · `ValidationCredentials` | offers | the S2S runner callbacks (`/internal/v1/validation/{cycleId}/…`, via the internalServer — not the public edge). Keyed by the CYCLE the pod was dispatched for, which is the only identity a runner has |

## Owns
- The **executions** store (now provisioning gates only) and the Temporal `Runtime` + the one workflow on it.
- The **build click's whole sequence** (`build`): mutex → repo → drawer pre-tag work → dependency hard
  gate → whole-spec gate + `v<N>` tag cut → supersede → milestone → run row → plan. The ORDER is the
  domain fact `build` owns; the two halves it does not own (the planning turn, the gate resolvers) are
  root ports.
- The **event plane** (`eventcore`): the platform's whole reaction to a pull request, a milestone-matched
  issue and a build terminal. It merges, mints and signals — the supervisor decides. Its three GitHub
  effects are a squash-merge, an issue in a milestone, and a build pinned to a merge SHA.
- The **milestone run** store: a run row per (org, project, milestone) — origin, small state, terminal
  reason, budget counters, validation verdict — and one **cycle record per dispatch** under it (kind,
  attempts, Job ref, branch, the pull request's number AND its page on the host, merge SHA). The pull
  request URL is stored as the webhook reported it, never composed from the repo row and the number:
  the repo URL is a *clone* URL, and a reader that assembles links from it encodes both the host's URL
  grammar and that spelling. The milestone **number** is the key; the title is kept
  only as the `v<N>` tag a `?tag=` query resolves through. Loop position is read from the latest cycle, and
  per-component build/deploy status is derived from OpenChoreo on read — neither is stored.
- **Delivery's agent SPEND**, in the `agent_usage_ledger`: an append-only entry per dispatch carrying the
  captured tokens, the USD stamped at capture (`cost_usd`, amended console ADR-0011), the SDLC phase and
  the version it paid for. Both capture surfaces mirror into it as part of their own stamp — `RecordUsage`
  on the cycle and on the execution — so `PhaseUsageRollup` reads the ledger and NOTHING else: adding the
  dispatch rows back would bill every token twice. `RecordUsage` stays the one cycle mutator NOT fenced on
  `ended_at IS NULL`: usage arrives from the terminal-log capture, and a cycle closes on the merge webhook
  seconds after its Job exits — fencing it would discard nearly every capture. Entries are keyed
  `(source, source_id)`, so a re-read of the same log updates one entry rather than adding a second.
- The **run loop** (`run`): one Temporal workflow per milestone, `run-<org>-<project>-<milestoneNumber>`,
  whose id is REUSED after a terminal run because a milestone sees sequential runs across its life. It
  owns the four budgets, the no-progress rule, the cycle ceiling, the validation cycle and settle — and
  nothing else: it detects no event and writes no issue.
- **Persistence**: every gorm in this domain sits at the ROOT (the fence `TestGormFencedToDomainRepository`
  draws), as single write-authority — `repository_execution.go` · `repository_coding_agent_log.go` ·
  `repository_run.go` · `repository_cycle.go` · `repository_usage_ledger.go` over the `execution.go` /
  `coding_agent_log.go` / `milestone_run.go` / `run_cycle.go` / `usage_ledger.go` entities. Their tables
  are `executions` · `coding_agent_logs` · `milestone_runs` · `run_cycles` · `agent_usage_ledger`.
  `run_cycle_logs` is retired (migration step is a tombstone; cycle logs are read from OpenChoreo /
  the observer, not Postgres). `usage_rollup.go` is a plain function over the ledger rather than a
  third store.

## Invariants — don't break
- **`task ⊥ run`.** The GitHub-facing Task surface and the run supervisor are peer sub-packages that never
  import each other, in either direction. Dispatch has exactly ONE door — a run works a milestone — so a
  `task` that could reach the supervisor would be a second door with the run's budgets bypassed.
  `TestTaskRunSplit` + `slice ⊥ sibling` both enforce it.
- **One active spec run per project.** At most one non-terminal (`planning`/`waiting`/`running`) `spec-build`
  milestone run exists per (org, project) — a partial unique index (`ux_milestone_runs_spec_active_v2`, created
  by the `milestone_runs` migration; AutoMigrate cannot express one) that admission hits with
  `INSERT … ON CONFLICT DO NOTHING`, so the invariant holds under concurrency and not merely under the
  endpoint's pre-check. Both answers are the same 409. `incident-adoption` runs sit deliberately outside
  the index and execute concurrently on their own milestones.
- **The version is claimed before it is planned.** The run row IS that mutex, so the build click admits it
  synchronously — supersede, mint the milestone, admit — and only then runs the planning turn, detached
  from the request. Admitting after planning would leave the mutex unarmed for the minutes an LLM turn
  takes, which is exactly the window a double-click lands in. A plan that cannot land settles the run it
  armed (`plan-failed`), so a failure never wedges the project behind its own mutex.
- **A version supersedes its predecessor, found through the run rows.** Before `v<N+1>`'s milestone exists,
  `v<N>`'s still-open issues are closed with a `Superseded by v<N+1>` comment — the agent work first, then
  the gates that were holding it — and then the milestone. The previous milestone is located by the NUMBER
  recorded on a run row, never by matching titles against GitHub (titles are renamable, and title filters
  are case-insensitive while create-uniqueness is not). This is what keeps the reconcile sweep sound: a
  superseded milestone holds no open `aep` issue, so the sweep's trigger never fires on it.
- **Every issue body is prose; nothing platform-side parses one.** That holds for planned Tasks, for
  dispatch gates and for the validation issue alike: the milestone is the version pin, LABELS carry every
  routable fact, and ordering is the "Depends on #N" lines the AGENT honours. Dedupe on re-plan is the
  title slug against the milestone's own issues, which makes reconcile additive-only and a crash re-run a
  no-op. Gates (`aep:provision` + `aep:dep/<slug>`, minted by `dependencies/provisioning`) and the
  validation issue (`aep:validation`) deliberately do NOT carry `aep` — a gate is a dispatch hold and the
  validation issue is a phase of the run, and neither may hold the settle predicate open.
  The corollary, and the trap: a read that NARROWS on `aep` cannot see either of them. So a decision that
  must weigh them (the auto-merge policy, which merges the validation cycle's pull request) reads the
  milestone's open issues UNFILTERED and decides on the labels itself. `?labels=` on the REST issues
  endpoint is AND, so there is no filter that returns both populations anyway — and a label predicate split
  across the fetch and the decision is one rule in two places, which is how the validation cycle's pull
  request once ended up declined as "not this run's work".
- **Milestone assignment rides issue creation.** A plan costs `1+N` content-generating requests against
  GitHub's 80-per-minute ceiling: one milestone create plus one issue create per Task. Never
  create-then-PATCH, and never a label pre-create per issue (`sourcecontrol` memoises the ensure).
- **Settled rows are never resurrected.** Every run and cycle mutator is a guarded update — fenced on the
  run not being terminal, or the cycle not being closed — and returns `(nil, nil)` when it changes no row,
  so a duplicate webhook or signal is a no-op rather than a rewrite of a recorded outcome.
- **A component's CR is provisioned immediately before its first build, by the FAN-OUT.** A cycle is
  scoped to a milestone and may touch several components, so no dispatch knows which component is about to
  be built; the merged-PR fan-out is the last point that does. It ensures the OpenChoreo Component CR (and
  emits any web-app runtime config) per component, then triggers — without it a first-ever component's
  build fails "Component not found". An unensurable component is not built: triggering a build for a CR
  that does not exist only fails later, and less clearly.
- **Every event-plane handler keys off a milestone run row.** It resolves the run first — by the agent's
  `aep/m<milestone#>-…` branch, by the milestone a payload embeds, by the cycle that landed a commit, or
  (for incidents and adoption) by the deployed version's run — and returns having written nothing when
  there is none. That gate is what keeps the plane's authority scoped to work the platform started, and
  what lets it share the `pull_request` and `issues` routing keys with other handlers without racing them.
- **Adoption makes an issue agent work — milestone membership alone does not.** The working set is the
  milestone's `aep`-labelled issues (`OpenNonGateWork`), and a milestone holds ledger issues too, so an
  adopted issue without the label is invisible to the dispatch predicate: the run starts, finds nothing,
  and parks forever. `AdoptIssue` therefore stamps `aep` on BOTH routes — the `aep:codingagent` webhook
  and the handoff's promote leg. The two labels record two different facts and neither replaces the
  other: `aep:codingagent` is the act of adoption, `aep` is its consequence.
- **A bare issue adopts into the version in flight when none is deployed.** An incident is raised by a
  deployment, so the SRE/RCA handoff routinely lands while the run that caused it is still short of
  `succeeded` — refusing there dropped the handoff permanently, because nothing retries it. The deployed
  version still wins when there is one; with neither, the caller gets `ErrNoAdoptableMilestone`, which
  the HTTP edge maps to a 409 rather than letting an actionable refusal leave as an opaque 500.
- **Adoption wakes a parked run; it does not rely on the webhook to do it.** The `aep` label adoption
  writes comes back as a suppressed echo, so a run parked on an empty working set would never hear about
  the work now sitting in front of it. `wakeIfWorkable` is the one definition of "this milestone can now
  be worked", shared by the `issues` webhook and adoption so the two cannot drift.
- **The event plane imports no workflow engine.** It detects, mints and signals; the supervisor decides.
  The dependency direction is enforced as a package boundary — the supervisor is reachable only through
  the `RunSignaler`/`RunStarter` ports, so no loop decision can be smuggled into a webhook handler.
- **A signal is a wake-up, never evidence.** The supervisor re-reads GROUND TRUTH before acting on any
  signal: the milestone's own issue counts at every cycle boundary, and the CYCLE RECORD (not the payload)
  to decide whether the agent's pull request landed — a human's pull request merging mid-cycle raises the
  identical signal. That is what makes a lost delivery cost latency rather than correctness, and it is why
  the wait state can be unbounded with cancel as its only expiry.
- **The supervisor counts its own budgets.** They are workflow state, written OUT to the run row for the
  read model and never read back: a replay must reproduce the same decisions without a database. The one
  budget it does not count is the automatic build re-trigger — that is the event plane's, derived from the
  WorkflowRuns themselves, and the supervisor reads the same runs rather than keeping a second tally.
- **Activities retry blips forever and answers never.** The supervisor's activities run under Temporal's
  default policy — unbounded, with backoff — because a supervisor that cannot reach GitHub should stall
  visibly rather than settle a run on a network blip. A failure that repeating cannot change is told
  apart wherever the supervisor touches source control and marked non-retryable, so it fails on
  its first attempt: the project deleted underneath a live run, a milestone or pull request removed on
  the host, a rejected credential. `sourcecontrol.IsPermanent` owns which failures those are — GitHub's
  secondary rate limit wears a 403 and is deliberately NOT one — and `run/errors.go` owns turning them
  into Temporal's vocabulary. The guard is per call site, so an activity added later that returns a
  source-control error raw is back on unbounded retry; `CloseMilestone` is the one deliberate exception,
  swallowing its error by contract and so never retrying.
- **Every terminal reason names exactly one failure class.** `redispatch-budget` is agent death (including
  a Job that exited without a pull request); `build-retrigger-budget` is a build that stayed red through
  its one automatic re-trigger with no fix issue to recover it; `fix-chain-budget` and `conflict-budget`
  bound the two recovery chains; `no-progress` is a green cycle that left the milestone unchanged;
  `cycle-ceiling` is the backstop over all of them. The validating phase contributes two, and they are
  two because they are different failures: `validation-failed` is a criterion that asserted and lost —
  a fact about the software — while `validation-unreported` is the agent merging its pull request
  without committing a report at all, which proves nothing about the software and is a breach of the
  runner contract. `ValidationVerdictFailsRun` / `IsValidationTerminalReason` are the executable copy of
  that pair. A run that settles for a reason outside this list is a bug in the loop, not a new state.
- **Settle closes the milestone; nothing branches on that.** Milestone state is display only, closed
  milestones still accept new issues, and a failed or cancelled increment leaves its milestone OPEN
  because the way forward from it is more work in the same version. A stray gate never blocks settle:
  gates hold dispatch, and with an empty working set they hold nothing.
- **A run that has never dispatched does not settle on an empty working set — it waits.** "Nothing
  left to do" means DELIVERED only in contrast to work the run actually did; with zero cycles behind
  it the same reading is indistinguishable from a milestone whose issues have not been minted yet,
  because the version is claimed before it is planned (above) and a poll can land inside that window.
  So the run parks in the unbounded wait and re-derives on every `issues` webhook and at the poll
  backstop. Three things end it: work arriving, a human cancelling, or — when the planning turn itself
  failed and no issue is ever coming — the plan path settling the row it armed with `plan-failed`.
  Those two writers cannot race: the plan path starts the supervisor only after planning returns, so a
  run that failed to plan has no workflow behind it, and the repository's non-terminal guard on
  `Settle` is the backstop if that ordering ever changes.
- **One task queue, one worker.** `run.WorkerWatcher` owns it: a task queue must be served by ONE worker
  that knows every workflow on it, and the run supervisor is the only workflow left. Two workers polling
  one queue with disjoint registrations would fail whichever tasks each picked up by accident.
- **Cancel is a signal, not a Temporal cancellation.** A cancelled context could not run the activities
  that record the outcome, so the run settles its own row and closes its own cycle on the ordinary path.
  Stopping the agent is the HTTP cancel surface's job: `runread.Commands.Cancel` signals the supervisor
  and then best-effort reaps the cycle's OpenChoreo Component via `CycleReaper` (immediate
  `DeleteComponent`, no retention — phase-08 Cancel B1). A failed reap does not fail the cancel; a BFF
  crash mid-cancel can leave the pod until the retention sweep. Natural finishes are NOT reaped on that
  path — retention/LRU owns those.
- **Echo suppression is `issues.*`-only.** Every label, comment and milestone assignment the platform
  writes fires an `issues.*` delivery straight back, so those handlers drop self-sender deliveries. It is
  deliberately NOT applied to `pull_request.*`: in App mode the coding runner opens its PR as the same
  `<slug>[bot]` login, and suppressing that would strand the run waiting for a PR that already exists.
- **Handlers are idempotent, without a seen-it table.** A redelivered webhook re-runs the handler, so
  merging re-reads the live PR first, minting passes a `DedupeKey`, and triggering a build counts the
  WorkflowRuns OpenChoreo already holds for `(component, commit)` — the same count that enforces the
  one-automatic-re-trigger budget, so idempotency and the budget can never disagree. Per-component build
  state is derived from OpenChoreo on read, never stored, which is why a counter column would be wrong.
- **A run-loop build reaches `OnBuildTerminal` through `eventcore`'s build sweep, and nothing else.** The
  `BuildTerminalObserver`'s other caller is the `codingagent` ExecWatcher, which sweeps `kind=build`
  execution rows — and the run loop records its cycles in `run_cycles`, so it mints none. The sweep derives
  everything per pass (live runs → the cycle's merge SHA → the merged PR's path diff → the component's
  WorkflowRuns) and reports only the NEWEST attempt, and only once that attempt is itself terminal. That
  last rule is what makes re-reporting safe: a terminal run stays terminal and is re-read every pass, so
  reporting an older attempt would spend the re-trigger budget twice and mint a fix issue while the retry
  was still in flight.
- **The build clone credential is staged once per fan-out, never per component.** It is ONE per-org
  object and OpenChoreo has no update verb, so staging is delete-then-create; staging inside the fan-out's
  per-component goroutines had them racing to delete and recreate the same object, and the loser dispatched
  a build with an empty `secretRef` that cloned anonymously and died at checkout against a private repo.
- **The kernel names no feature.** The root holds only types/ports/Temporal infra; it never imports a
  sub-package (`root ⊥ slice`), and the domain never imports `internal/feature/*`.
- **`*run.Supervisor` stays a nil-safe concrete type**, not an interface, at the composition root — the
  event plane and the build click both hold it unconditionally, and a degraded boot (Temporal down) has to
  be a logged no-op rather than a nil check at each call site.
- **The run progress stream is ONE connection over every cycle, and only a terminal run settles it.**
  `stream-run-progress` (`GET .../runs/{runId}/progress`, `text/event-stream`) carries a `cycle` frame per
  cycle record and a `line` frame per agent-log entry, each line stamped with its cycle id, its 1-based
  cycle index and an **emitter chip** (`main` | `subagent`) so the console renders one accordion section
  per cycle and can tell the run's main agent from the work it fanned out with the Task tool. The frame
  kind rides in a `type` field inside the `data:` payload so it passes the shared agent-stream parser.
  A live run — including one parked in `waiting` — holds the stream open indefinitely; a terminal run
  streams its history, sends `done` + `[DONE]`, and the server closes. The server keeps no cursor; the
  client dedups by cycle id and `(cycleId, seq)`.
- **Agent logs are read, never stored.** Three sources answer, in order: live OpenChoreo pod logs while
  the Component exists, the observability archive while the Component is retained, and a synthetic
  `logs_unavailable` line when the Component is gone or no observability plane is configured. An empty
  stream and a lost log look identical to a reader and mean opposite things about the agent, so the
  platform never lets "gone" render as "silent". `CycleLogReader` serves the run-progress stream;
  `codingagent` owns the read path and writes no log text to Postgres. The one thing taken from a
  terminal pod's log is the runner's token-usage line, stamped onto the cycle row — accounting, not
  logging. `coding_agent_logs` remains for legacy execution rows; milestone cycles never used it.
  (`run_cycle_logs` is retired — do not revive a Postgres sidecar for cycle logs.)
- **A project delete purges the WORK and retires the SPEND.** `runs` and `run_cycles` are working state and
  go, so a recreated same-named project cannot inherit a timeline its repo never had; the
  `agent_usage_ledger` is not purged by anything — `RetireByProject` stamps its live entries instead, and
  that stamp is the only lifetime discriminator there is. It runs FIRST and aborts the cascade on failure:
  rows deleted while their spend is still attributed to a live project is the one combination no retry can
  repair. `contracts.UsageScope{ProjectID, Retired}` is how the roll-up hands the two lifetimes back, so a
  slug reused after a delete reads as two Usage cards rather than one inflated one. Spec-turn spend has no
  such marker (`agent_turns` is never purged and never retired) and stays attributed whole to whichever
  lifetime is current — a known asymmetry the spec domain owns.
- **The task-log stream is one connection, no server cursor.** `stream-task-log`
  (`GET .../tasks/{issueNumber}/log`, `text/event-stream`) carries a Task's whole live state — `task` /
  `execution` / `line` / `done` frames, the frame kind in a `type` field inside the `data:` payload so it
  rides the shared agent-stream parser. The server buffers no history and keeps no cursor; the client
  dedups and a reconnect re-derives. It settles when the Task's ISSUE closes, because nothing further will
  arrive on it. It is the per-ISSUE view; a run's own feed is `stream-run-progress`.
- **`derivedStatus` is the issue's own state, and only that.** Two values — `pending` (open) and `merged`
  (closed) — both deliberately members of the retired ten-value vocabulary, because the console consumes
  the field UNTYPED and a chip keyed on an unknown string renders as nothing. Anything richer belongs on
  the run's cycle timeline, which is where the loop's real position lives.
- **Validation is the run's last CYCLE and never builds.** The supervisor mints the validation issue at
  deployed-green (never at plan time — an issue nothing can work until every component deploys would hold
  every cycle boundary open), dispatches one cycle at it with `AEP_TASK_KIND=validation`, and reads the
  committed report back as the run's VERDICT. The acceptance oracle
  `specs/validation/validation-criteria.json` is read-only input authored in the design phase (spec domain).
  **ONE validation issue per version, filed into the version's milestone by the create itself** — like a
  Task, it carries no version label, because the milestone is the pin. Per version and not per project:
  the body embeds the criteria as they stood at mint time, so adopting an older version's issue would
  hand this version's agent the wrong oracle, and re-filing it would erase it from the ledger of the
  version it actually validated. The version's own open issue is looked up by milestone, and only there.
- **A version can be judged more than once, and the NEWEST run owns its verdict.** `revalidate` is the
  third run origin (`POST .../builds/{tag}/revalidate`): a fresh run over an already-shipped version's
  milestone that ENTERS THE LOOP AT VALIDATION, because its working set is already empty. Nothing is
  rebuilt to ask the question. What follows the verdict is the loop's ordinary behaviour, selected by ONE
  number — the run's validation-attempt allowance: at 1 the allowance is spent by the first fatal verdict,
  which settles the run *before* it reaches the repair mint, so no work is filed and nothing is rebuilt;
  at the default the full chain runs (issue per failed criterion → coding cycle → builds → validate
  again). There is deliberately no separate "should it repair" flag — that ordering is the switch.
  Three guards refuse it up front, each beside the collaborator that can answer it: a live run on the
  milestone (409 — unlike adoption there is nothing to hand off to it, and the revalidate origin sits
  outside the spec-run mutex so nothing in the database would refuse a second row), open work in the
  working set (409 — the loop would build it, not re-check it), and no acceptance oracle (422 — a run
  with nothing to validate concludes `skipped`, which would overwrite a real verdict).
- **The overview's build stage reads the newest SPEC BUILD; its validation chip reads the newest run on
  THAT milestone.** Only a spec build advances the project's version, and the other two origins work an
  existing milestone that may be any version's — so picking the newest row outright let a revalidation
  (or an incident adoption) on an older version walk the reported version backwards. Both are in-memory
  scans of the run list the status poll already holds, so neither costs a query.
- **The list read returns three populations, and hides one** (`task/reads.go` `ListByTag`, the read-model
  boundary). Every row carries the label-derived `executorClass` the console sections on: `coding` (agent
  work), `provision` (a dispatch gate, which the console renders as a hold banner rather than a row), and
  `ledger` — a bare human issue that joined the milestone carrying none of the platform's labels. The
  **validation issue is always hidden**: it is a phase of the run, not an implementation task, and its
  verdict rides `deploy.validation` on the project status and the version's run story. `get-task` and
  `stream-task-log` still serve it by issue number.
  A LEDGER issue is returned by the milestone-scoped read and only by it — the untagged read is two label
  queries, and a ledger issue is defined by carrying no label to query on. Milestone membership is the only
  handle there is.
- Platform-wide rules (tenant gate, secrets fence, persistence-in-domain) → [../../README.md](../../README.md).
