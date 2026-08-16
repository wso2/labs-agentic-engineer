# ADR-0017 — The platform owns deploy

**Status:** Accepted · **Supersedes:** the AutoDeploy assumption recorded in
`internal/projects/component_service.go` and the retired `trait_sync.go` · **Refined by:**
[ADR-0019](ADR-0019-deploy-order-follows-the-hard-wiring-edges.md), which orders the stage into waves —
the promote described below is what happens within one wave

## Context

Every OpenChoreo Component the platform created carried `autoDeploy: true`. A build's last step
posts a Workload, OpenChoreo's controller cut a ComponentRelease from it and bound that release into
the project's environment on its own. The platform never performed a deploy; it only observed that
one had been asked for.

Four consequences, all of them the same root cause — **the platform could not order anything around
a step it did not perform**:

1. **Validation raced the deploy.** The run supervisor minted its validation issue at "deployed-green",
   but the predicate that got there was builds-green. `RunPhaseBuilding`'s own comment said so: *"a
   green WorkflowRun means the deployment has been asked for, not that it has landed. Nothing in this
   loop observes a ReleaseBinding."* The validation agent asserted against whatever happened to be
   serving — possibly the previous release.
2. **A protected API served unauthenticated on first deploy.** The `jwtAuth` policy lives in
   `ReleaseBinding.spec.traitEnvironmentConfigs`, which cannot be written before the binding exists —
   and the binding was created, rendered and served by the controller. `trait_sync.go` recorded this
   as an accepted cost: *"Protected components keep autoDeploy: true, accept the short first-deploy
   exposure window."*
3. **Three deploy-time effects were orphaned.** The cross-project access grant, the SPA
   `env-config.js` emit and the trait re-emit all hung off `ExecWatcher`, which sweeps `kind=build`
   execution rows. The run loop records cycles in `run_cycles` and mints none, so none of them fired
   for anything it built. Only the traits half was rescued, by bolting a `SyncAPITraits` activity
   onto the build rail.
4. **A failed deployment failed nothing.** A binding stuck `Ready=False` minted no issue, named no
   terminal reason, and the run settled `succeeded` over a version that never came up.

## Decision

**Components carry `autoDeploy: false`, and the run supervisor performs the deploy as a stage of the
cycle**, between builds-green and validation:

```text
merge → build fan-out → builds green → DEPLOY → Ready → (working set empties) → validation
```

The deploy is `EnsureRelease` at the merge commit, then ONE `ApplyReleaseBinding` carrying the release
pin, the trait environment configs and the workload overrides together. The stage then polls each
binding's Ready condition.

Three supporting decisions follow from it:

- **One object, one writer.** `projects.DeploymentService` is the only writer of a user component's
  ReleaseBinding. It absorbed what three services used to patch on three different triggers (trait
  configs, `env-config.js` files, user env vars), each soft no-opping when the binding did not exist
  yet and each relying on somebody else to retry. This was forced, not merely tidy: a writer that
  PUTs the object must carry every field it owns, or it silently drops the others'.
- **The desired state is a pure projection.** `DesiredDeploymentFor` turns design facts into the two
  objects the platform owns — the Component CR's trait shape and the binding — so both halves come
  out of one function and cannot disagree. The trait shape is asserted pre-build because a
  ComponentRelease freezes it; the per-environment config lands at deploy because it needs a release
  to bind.
- **The deploy stage gets the loop's second deadline.** A WorkflowRun always terminates, so
  `awaitBuilds` can wait forever safely. A ReleaseBinding never does. On expiry the components that
  never came up become an ordinary fix issue, so the deadline introduces no failure class the
  terminal reasons do not already name (`deploy-budget`).

## Evidence

The blocking risk was a claim recorded in `trait_sync.go`: *"autoDeploy is required because OC's
project→environment→RB binding logic drives initial RB creation; BFF-managed RBs without autoDeploy
are not supported by OC."*

It is stale. The repository already ships the pattern for the ephemeral coding-agent component
(`delivery/codingagent/oc_dispatcher.go`: `AutoDeploy: false` plus BFF-driven
`EnsureWorkload → EnsureRelease → EnsureReleaseBinding`). It was then verified end to end for a USER
component against a live single-cluster OpenChoreo, driven through this service's own client:

| Step | Result |
|---|---|
| Component created with `autoDeploy: false` + trait shape | pass |
| Release cut from a build-posted Workload with autoDeploy off | **pass** — the make-or-break step |
| Binding written with the pin AND `jwtAuth` config in one call | pass |
| Binding reaches `Ready=True` | pass |
| Re-pin to a second release (create → 409 → PUT) | pass |
| Binding Ready again after the re-pin | pass |
| Unauthenticated request on the component's FIRST ever serve | **401** |
| Same request with a valid Thunder token | 200 |

The last two are the point of consequence (2): the exposure window is closed by construction, because
the policy was in the binding before OpenChoreo ever rendered it.

## Consequences

- Validation now asserts against a version that is genuinely serving. This is a behaviour change for
  every spec run, and the reason the change is worth its size.
- A deployment that never comes up is a named, recoverable failure: one fix issue per component, an
  ordinary fix cycle, and `deploy-budget` when nothing recovers it.
- The supervisor mints an issue, which it does nowhere else. It reaches the event plane through a
  port to do it, because a deployment is the one platform failure with no webhook behind it.
- Two half-backstops (the retired trait drift watcher and the `env-config.js` sweep) collapse into
  one `ConvergeWatcher` that re-asserts the whole binding — and a config edit converges through the
  same verb a deploy uses, so the two can never drift.
- `EnsureComponent` became an upsert. It has to be: the trait shape is frozen into the next release,
  so a design edit that has not reached the CR before the build is an edit the release silently
  drops — and an existing component still carrying `autoDeploy: true` would have OpenChoreo promoting
  releases underneath the deploy stage.

## Alternatives rejected

- **Keep AutoDeploy, have the supervisor merely WAIT for Ready.** Fixes consequences 1, 3 and 4 for a
  fraction of the risk, and was the planned fallback had the spike failed. Rejected because it leaves
  the auth window open, which is the one consequence with a security cost.
- **Read `Component.status.latestRelease` instead of cutting the release.** Reintroduces a race with
  the controller and makes the deploy non-idempotent under retry. Cutting the release under a
  commit-derived name means a retried deploy activity re-pins the same release.
- **Read-modify-write only the fields the deploy stage owns.** Smaller, and it works, but it
  perpetuates split ownership of one object — which is what produced the original mess.
