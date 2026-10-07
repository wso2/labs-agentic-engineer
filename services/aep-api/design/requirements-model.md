# The requirements model in aep-api

How aep-api reads per-feature requirements and builds and validates by
feature. Decisions: ADR-0039, ADR-0040, ADR-0041. Terms: `CONTEXT.md`
§ Requirements model.

## Reading the requirements — `internal/platform/reqspec`

- `Parse(files)` → `Spec`: features (stories, needs, assumed count, blocking
  questions, retired IDs), product-wide items, retired features and P items,
  and the problems the save gate reports (`Problems()`).
- `Basis(files, F)` — what a feature's design reads. `FeatureLines(files, F)` —
  a feature's lines as a build keeps them. `PlanBuild(spec, built, pick,
  unavailable)` — what a selection carries and what it refuses.
- Held to `packages/contracts/requirements/acme-expenses` (`expected.json`,
  `feature-lines.json`, `basis.json`), which the console's reader shares.

## The save gate — `internal/spec`

`SaveSpec` (the build click's tag cut) reads requirements, design and
acceptance at one commit and refuses the tag on: requirement ID problems;
designs or roles citing a feature instead of its stories, or stale stories; an
acceptance file missing a carried story's tag; a feature out of date
(`staleFeatures`: its basis now vs at its last design run, from
`CompletedFlows`); a pick it cannot carry (`build_selection.go`). The tag's
annotation records the plan (`scopeBody` / `parseScope`).

- `BuildScopeAtTag` — the stories, features, P items and component claims a
  version carries (the planner's milestone scope).
- `ListVersions` (`GET /versions`) — what each version built, with each
  feature's lines at its tag.
- `ValidationScope` — what a version validates (built so far, minus held back)
  and the versions before it.
- `TagRepair` — a repair version `<of>.<n>` at the fixed version's commit.
- `SpecState` (`GET /spec/state`) — each designed feature's basis at design
  time, and the attached documents.

## Building — `internal/delivery/build`, `delivery/task`

`build.Service.Run` carries the pick into the tag; `dependencyGate` blocks the
features an open external dependency's component serves. The plan turn reads
`tags/<version>` and cuts one Task per feature per component plus a
foundation Task (`plan_tap.go` links them in dependency order and stamps each
with its feature's stories). `Repair` files the fixed version's final failures
into the repair milestone and starts the run without planning.

## Validating — `internal/delivery/validation`, `app/run_adapters.go`

`runValidation.judge` reads one attempt's report within the version's scope
(`Scope`, story tags read from the acceptance files at the same commit) and
against the previous validated version's final attempt (`Baseline`). The
verdict, digest and regression count come from it; repair issues are filed by
standing (regression / still failing / plain). Office uploads are converted
to markdown by `internal/platform/officetext`.
