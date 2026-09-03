# ADR-0026 — Deploy reconciles the version

**Status:** Accepted · **Refines:** [ADR-0017](ADR-0017-the-platform-owns-deploy.md) (the platform
still owns the deploy verb) · **Supersedes one claim of**
[ADR-0019](ADR-0019-deploy-order-follows-the-hard-wiring-edges.md) (the deploy set, and the scope the
waves are planned over)

## Context

A version shipped that was not running.

Project `track-each-hire7321`, version `v1`, two components. Cycle one merged both; the API's build
went green and the web app's went red. The supervisor read the red verdict, returned before its
deploy stage, and the event plane filed the web app's fix issue. Cycle two merged the fix — two
files, both under the web app — so the merge's path diff named the web app alone. It built, deployed,
and the milestone emptied. The run settled **succeeded**, minted the version's validation task and
closed the increment.

The API was never promoted. No `ComponentRelease`, no `ReleaseBinding`, no pod. Its image had existed
since cycle one. Every cycle in that run was correct on its own terms, and nothing in the sequence
was a lie — the predicate was:

```text
"deployed-green" was inferred from   the working set is empty
                                   ∧ the last cycle ended green
```

Neither conjunct is a statement about the API, so neither could contradict it. Two shapes combined to
produce it:

1. **The deploy set was the cycle's path diff** — `ListPullRequestFiles ∩ ComponentPaths`, classified
   only at that cycle's merge SHA. So *what is serving* was a function of which files a fix happened
   to edit. A component whose files stop changing is in no later deploy set, whatever state its
   release is in.
2. **A red build returned before the deploy stage.** The cycle that could have promoted the API was
   the one whose sibling failed, and it never reached the promote.

The consumer then went live against a provider with no address at all — the case
[ADR-0019](ADR-0019-deploy-order-follows-the-hard-wiring-edges.md) named as "an assumption, not a
proof". The assumption was that the run loop could not produce it. It could, in the most ordinary way
available: one red build.

## Decision

**The deploy stage reconciles the VERSION against what has been built, on every cycle, red or
green.** It no longer promotes a cycle's diff.

Two states per component, re-derived from ground truth on every pass:

```text
desired(c)  the release the component's newest SUCCEEDED build would cut
              — OpenChoreo WorkflowRuns · delivery.ReleaseNameFor
actual(c)   the release its ReleaseBinding pins, and whether that is Ready
              — ReleaseBinding spec.releaseName + the aggregate Ready condition
```

and five states over the difference:

| state | condition | the pass |
|---|---|---|
| `serving` | pinned = desired, Ready | nothing owed |
| `behind` | pinned ≠ desired (including no binding) | the only state written to |
| `converging` | pinned = desired, not Ready yet | waited on |
| `held` | behind, a hard provider not serving | left alone, named |
| `unbuilt` | no succeeded build at any commit | left alone |

**Promotable, not green.** This is the answer to "A needs B; A is green, B is red — deploy A?"

```text
promotable(c) = behind(c) ∧ ∀p ∈ HardConfigEdges[c]: serving(p) ∨ promotable(p)
```

Anything else behind is `held`, carrying the providers it waits on. The rule is a fixpoint, not one
pass: a consumer waiting on a provider that is itself held has to fall out too. The edges are read
over the **whole design** — `spec.HardConfigEdges` already returns exactly that — which is what makes
a provider with no serving release an unsatisfied edge instead of the assumed-satisfied one that
published the web app.

`held` and `unbuilt` are not deploy failures and are never waited on. A red build has already minted
its fix issue; a component nobody has written yet has its development issue open. Filing a deployment
bug against either would send an agent to debug a deployment that is healthy and has simply not
happened. Before this, neither state could be named at all: the incident's API was behind for fifty
minutes and no code path had a word for it.

**Running on a red cycle is the deliberate half.** It is what makes *what is serving* a monotone
function of what has been built, rather than of cycle luck. The promotable rule is what makes it
safe: the only components written are ones whose dependencies are met. A provider's DB wiring gets
exercised in cycle one instead of cycle three.

**Idempotent by construction.** A fully serving version has nothing behind, so a pass writes nothing,
plans nothing, starts no deadline and never consults the deploy gate. That is the property that lets
it run every cycle, and it is the one ADR-0019 already engineered into the verb: `EnsureRelease` is
idempotent by read-back and the binding write is an upsert.

**And the delivery gate asserts it.** `serving(V) := ∀c ∈ design: state(c) = serving`, read fresh at
the cycle boundary — never carried from the reconcile, because between the two the world may have
moved and the whole value of the gate is that it asserts what is true now.

```text
working set empty ∧ serving(V)      mint the validation task · settle succeeded
working set empty ∧ ¬serving(V)     settle FAILED · version-incomplete, naming each
                                    component and its state
working set non-empty               next cycle, as before
```

By the invariants above the second row should be unreachable — a behind component whose providers are
met is promoted by the cycle's reconcile, and a failed deployment mints a fix issue that keeps the
working set non-empty. It exists because the alternative to unreachable is **silent**: the shape it
replaces settled such a version succeeded and filed a validation task against software that was not
running. A gate that should never fire is worth having when the thing it catches is
indistinguishable from success.

## Shape

```text
agent stage → merge → awaitBuilds(this SHA, PR diff)     unchanged: the CYCLE verdict
                        any red → build verdict red      (fix issue minted, as before)
                    ↓
                    ReadVersionState                     every design component, classified
                    ↓
                    reconcileVersion                     runs on EITHER build verdict
                      plan   := behind ∩ promotable, levelled by hard edges
                      write  := wave by wave, each at its OWN commit
                      wait   := promoted ∪ converging, ONE deadline from the first write
                      converge soft facts over waited ∪ already-serving, no promotion
                    ↓
                    cycle result = build verdict × deploy verdict
```

Three consequences of the shape are worth stating outright:

- **The commit is per component.** `delivery.DeployTarget` pairs each component with the commit its
  own newest green build was cut from. One commit for a whole list was only ever right when the list
  was what a single merge built.
- **The result is a pair folded to a scalar, build verdict first.** A cycle can now be red *and* have
  a deploy failure. Both mint their work; `lastResult` takes the build verdict because the deploy
  verdict is downstream of it, and `lastResult` only decides the next cycle's kind and the terminal
  reason at an empty boundary — where both would name a budget that has already produced its issue.
- **The converge covers what was already serving, not just what this pass wrote.** Soft facts flow
  from consumer to provider: promoting a web app in cycle two has to finish the wiring of an API
  promoted in cycle one. A converge scoped to the pass would leave that API permanently unaware of
  the SPA it serves.

## Evidence

| Claim | Evidence |
|---|---|
| Cycle two's diff named the web app alone | `gh api repos/asdlc-repos/track-each-hire7321/compare/main...aep/m1-c2` — two files, both under the web app |
| The API had no binding while the version was "delivered" | `kubectl get releasebinding -n default` — no `…-onboarding-api-development` object |
| The web app knew it was unsatisfied | `releasebinding …-onboarding-webapp-development -o json` → `status.pendingConnections[0].reason` names the missing API |
| The API's image existed from cycle one | its cycle-one `WorkflowRun` is `WorkflowSucceeded` |
| A hand-written binding brought it up in ~1 minute | `ComponentRelease` cut through `POST …/generate-release`, binding `Ready=True` |
| The console had said so all along | the Development column accounts for every design component, and rendered the API as "not deployed" |

The last row is why this ADR adds no console change: the read model was already honest. What was
wrong was the loop's own predicate, which is the only thing that decides whether a version ships.

## Consequences

- `PollCycleBuilds` stays diff-scoped and SHA-scoped, which is correct for the cycle verdict. It no
  longer feeds the deploy — `awaitBuilds` answers a verdict and nothing else.
- `ReadVersionState` is an activity, so the classification lands in workflow history the way the wave
  plan does: which components were behind, which were held, and what each was waiting on is readable
  off the run rather than inferred from what the stage went on to write.
- `RunStatus` carries the last `VersionState` as live status, not as a column. It is a fact about the
  world; what outlives the run is the terminal reason plus the settle log naming the components.
- `ReleaseBindingSummary` gains `ReleaseName` and `BuildRunInfo` gains `CommitSHA` + `StartedAt`.
  Both reads existed; both were one field short of being able to compare desired with actual. The
  commit is read from the WorkflowRun's own spec, never parsed out of its name —
  `k8sname.Bounded` truncates a long readable head and appends a digest, so parsing is sound for
  some projects and silently wrong for others.
- A deliberate `Undeploy` binding reads as `serving`: nothing is owed. Promoting over it would be the
  platform overruling the person who asked for it, and refusing to deliver would fail a run over the
  same decision.

## Cost accepted: in-flight runs do not survive the upgrade

`ReadVersionState` is a new activity call ahead of existing ones, in two places, and three deploy
activities are renamed. A dev or task run inside its deploy stage when the worker restarts fails on
non-determinism and stalls as a workflow task failure — not as a settled run. Runs that have not
reached deploy replay unchanged.

ADR-0019 took the identical edit at the identical cost and deliberately did not reach for
`workflow.GetVersion`, because that would keep the diff-scoped promote alive for old histories — the
very code being removed. Deploying this drains rather than migrates: ship it when no dev or task run
is inside its deploy stage, and cancel the ones that are.

## Not done, deliberately

**Undeploy.** A component removed from the design is not in the reconcile and keeps serving. That is
a separate decision about withdrawal semantics.

**A converging provider does not satisfy its consumer.** Only a serving release proves an address
exists, so a consumer whose provider is still rolling out is held and costs one cycle. The
conservative direction, and rare: within one stage the provider's wave is waited on before the
consumer's is planned.

**Promotion outside a run.** `ConvergeWatcher` keeps re-asserting only bindings that already exist.
Moving what is serving under a validation would break the ordering ADR-0017 exists to guarantee, so
promotion stays the stage's alone.

**Gating the no-cycles ending.** A run whose planning turn minted nothing settles succeeded without
passing the gate (`onEmptyWorkingSet` case 1). It never dispatched anything, so it claims to have
delivered nothing; widening the gate to cover it would fail rebuilds of versions whose work is all
closed. Worth revisiting if a version is ever seen shipping through that path.
