# ADR-0019 — Deploy order follows the hard wiring edges

**Status:** Accepted · **Refines:** [ADR-0017](ADR-0017-the-platform-owns-deploy.md) (the deploy stage's
internal shape; everything else in it stands)

## Context

ADR-0017 gave the platform the deploy verb. The stage it describes promotes a cycle's whole component
set at once and waits for every binding to report Ready.

That is not enough, because some of what the platform writes into a component's binding is only
knowable once *another* component is already serving. OpenChoreo assigns a component's external
address when its binding renders; nothing before that can be asked for it. Two facts depend on one:

- A web app reads its backend's address out of `window._env_` (the `env-config.js` the binding mounts).
  Its `src/env.ts` throws at module load if the key is absent, so a SPA published without it serves a
  **blank page**.
- A protected API's CORS allowlist is the project's SPA origins, and an OIDC resource needs the SPA's
  callback URL registered — both of which need the *consumer's* address.

The stage as built handled this with a two-pass fixpoint: promote, wait for Ready, then recompose and
promote again. Three things were wrong with it, and all three were observed live on project
`todooo1414`:

1. **The second pass re-promoted.** It passed the same merge commit, so it re-cut a release that
   already existed. openchoreo-api answers that with a bare `500` — not the `409` the client was
   written against — so the pass failed, and under Temporal's unbounded retry it failed every ~100
   seconds for twenty minutes with the run wedged mid-stage. Validation never started.
2. **A soft fact vetoed a hard one.** `runtimeconfig` graded the SPA's own-URL callback registration
   as required. That URL cannot exist before the SPA's first write, so the whole `env-config.js` was
   withheld — including the OIDC `client_id`, which had been resolved all along and owed nothing to
   any component being up.
3. **The consumer was published knowingly misconfigured.** Even with a working second pass, the SPA
   goes live with no config and is repaired afterwards. Anyone visiting in between gets the blank
   page. What actually repaired it was `ConvergeWatcher`, the 10-minute drift backstop — a component's
   hard dependency being satisfied by a sweep whose stated job is *"drift no event causes"*.

## Decision

**Wiring edges are graded, and only the hard ones order the deploy.**

```text
HARD  the provider's address is stamped into the consumer's own start-up config.
      Orders the deploy. Must form a DAG.
SOFT  the fact flows the other way — a provider learning about its consumer.
      Orders nothing. Free to cycle.
```

`spec.HardConfigEdges` is the single authority for the rule, consumed by both the composer that needs
the addresses and the planner that orders around them. A hard edge is deliberately **not** "any
dependency": a service reaching a sibling service is resolved by OpenChoreo's own connection
mechanism, so the platform stamps nothing and has no reason to serialise the two — ordering those
would refuse two services that call each other, which is an ordinary shape.

The stage becomes:

```text
builds green
   │
   ├─ wave 0   no unsatisfied hard edge        → promote → wait Ready
   ├─ wave 1   providers now have addresses    → promote → wait Ready
   │  …        topological levels of the hard-edge DAG
   │
   └─ converge  re-assert every binding with the soft facts, NO promotion
                                                            → wait Ready
   ↓
validation
```

Three properties follow:

- **No component is published before the hard edges WITHIN ITS CYCLE are satisfied.** The blank page
  stops being a window to be shortened. It also drops the consumer from two rollouts to one: its
  config is right the first time it is written. The scope of that claim is exact and worth stating —
  the planner orders only edges whose provider is in the same deploy set, on the ground that a
  provider outside it is already deployed and already has an address. A consumer whose provider
  exists in the design but has never deployed at all would still be published unconfigured. That is
  not reachable from the run loop (the deploy set comes from a build fan-out that is red if any
  component failed, and a first build builds everything), but it is an assumption, not a proof.

  > **Superseded by [ADR-0026](ADR-0026-deploy-reconciles-the-version.md).** The assumption was
  > false, and the counterexample shipped: a red build stops the fan-out being green, but the cycle
  > that follows it rebuilds only what its own fix touched — so a component green at the earlier
  > commit is in no later deploy set, and a consumer's provider can exist in the design having never
  > deployed. Project `track-each-hire7321` delivered v1 that way, with its API unbound. The planner
  > now reads the whole design's edges over the version's own state, so a provider with no serving
  > release is an unsatisfied edge rather than an assumed-satisfied one. Everything else in this ADR
  > stands: the grading of edges, the waves, the commitless converge, the one deadline, the permanent
  > cycle refusal.
- **The converge carries no commit.** `Deploy` with an empty commit SHA re-asserts wiring at whatever
  release is already serving (the verb a config edit has always used). Nothing is re-cut, so the
  pass cannot fail on a release that already exists — the wedge in (1) is removed by the shape rather
  than tolerated.
- **A hard-edge cycle is permanent, not slow.** Two components that each need the other's address
  cannot both go first, and retrying does not change that. The planner returns
  `delivery.ErrDeployPermanent` naming the edges; the loop converts it to an ordinary
  `cycleDeployFailed` so the fix work is filed and the run settles on the deploy budget. Returning it
  raw would fail the workflow before either could happen, leaving a non-terminal run row — the same
  wedge this stage exists to stop producing. Note this is future-proofing rather than a live
  property: today's edge rule runs webapp→service only, and a component has one type, so the shipped
  graph cannot actually produce a cycle. The refusal is asserted on the edges directly
  (`TestWavesFromEdges_HardCycleIsPermanent`) so it holds for whatever edge kind is added next.

The stage keeps ONE deadline across all waves and the converge. What a version is owed is a time to be
serving; a per-wave budget would silently multiply that allowance by however many levels a design
happens to have.

**`EnsureRelease` is idempotent by read-back, not by status code.** Because openchoreo-api cannot
distinguish "already cut" from "broke" in its response, the client asks the question that does have an
unambiguous answer: it reads the release back after a refused write and treats a release that is there
as the write having already happened. This is independent of the wave change and required with or
without it — a deploy activity that fails on its third component retries all three.

## Evidence

The diagnosis came from the live cluster, and each claim has an artefact behind it:

| Claim | Evidence |
|---|---|
| The second pass re-cut an existing release | `POST …/generate-release` → `status:500` in the openchoreo-api access log, every ~100s from 09:01 |
| Those releases existed from pass 1 | `todooo1414-todo-api-cd8d83b199ae-7d5c2c31` present, binding `Ready=True` |
| The 500 is generic, so the client cannot classify it | 58-byte body, identical to any other fault |
| The SPA was repaired by the watcher, not the stage | the writes that landed carry `release:""` (the converge path) and are exactly 10 minutes apart |
| Service addresses need no hash to be known | `development-default.openchoreoapis.localhost` + `/todooo1414-todo-api-http` — `<env>-<dataplane>` and `<project>-<component>-<endpoint>` |

The last row is why the waves are an intermediate rather than the end state (see below).

## Consequences

- `runtimeconfig`'s `ready` flag means "the keys the SPA starts with are present". The consumer-URL
  registration retries on the converge pass and on the converge watcher, and never holds back a file.
- `ConvergeWatcher` goes back to being only what it says it is. It was silently load-bearing for a
  hard dependency; now the stage satisfies that itself and the watcher covers drift.
- The wave plan is an activity, so it is visible in workflow history: a stage that promoted in the
  wrong order can be read off the run rather than inferred from a blank page.
- Adding a new stamped-config shape (a worker handed a queue's address, say) means adding it to
  `spec.HardConfigEdges` — and the composer and the deploy order follow it at once, because they read
  the same list.

## Cost accepted: in-flight runs do not survive the upgrade

The stage gained an activity call BEFORE existing ones, which is not a replay-safe edit: a run already
inside the deploy stage when the worker restarts fails on non-determinism rather than resuming. Runs
that have not yet reached deploy replay unchanged.

`workflow.GetVersion` is the sanctioned fix and is deliberately not used — this codebase has never used
it, and here it would mean keeping the two-pass promote alive for old histories, which is precisely the
code being removed for wedging every run that reached it. Deploying this drains rather than migrates.

## Not done, deliberately

**Allocating addresses instead of observing them.** The waves exist because an address is discovered
after a render. A service's external address is in fact fully determined by facts the platform already
holds — no hash — so if OpenChoreo projected it before bind, or accepted a declared vhost, every hard
edge would resolve at *plan* time and the waves would collapse to a single pass. Deriving OpenChoreo's
naming convention inside this service would work today and is rejected: it is a hidden coupling that
would break silently. Asking OpenChoreo to expose it is the right next step.

**Same-origin composition.** Mounting the API under the SPA's host at `/api` deletes both edges at
once — relative fetches need no address, and same-origin needs no CORS. It is the largest available
simplification, but it is a product decision about URL shape and it does not cover cross-project
dependencies.
