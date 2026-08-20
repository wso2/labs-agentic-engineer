# ADR-0020 — External dependency values are a deploy gate, not a build gate

**Status:** Accepted · **Builds on:**
[ADR-0017](ADR-0017-the-platform-owns-deploy.md), which made the deploy a stage
the platform performs — this decision is what that stage refuses to proceed past

## Context

An external dependency's values are not needed to cut a version or to write the
code that consumes it. The coding agent needs the dependency's declared
environment-variable *names*, which the platform already derives and stamps into
the design; the values themselves are consumed only by a deployed application.

Requiring every credential before Build therefore made unrelated coding work
wait on secrets that nothing would read for another twenty minutes. The build
drawer that collected them also held its form state locally, with no route and
no URL: it could not survive a reload, and could not be sent to the colleague
who actually holds the key.

OpenChoreo does not supply the readiness signal this needs, and two of its
behaviours shape the whole design:

- A config key **present with an empty value** renders as an empty string, but a
  key **absent** with no schema default is a hard rendering failure — the
  binding does not render at all. Authoring every declared key is mandatory,
  not defensive.
- A binding reaches `Ready` regardless of whether its secret backend holds
  anything, because the rendered `ExternalSecret` is health-checked as an
  unknown resource and reported healthy unconditionally.

Both are pinned with citations and an OpenChoreo version in
[`services/aep-api/internal/projects/design/openchoreo-resource-binding-behavior.md`](../../services/aep-api/internal/projects/design/openchoreo-resource-binding-behavior.md),
so a version bump has an obvious place to be re-verified.

## Decision

**Missing values for an `external` dependency do not block Build. They block
deploy.**

Provisioning authors the dependency with every declared key present, Build cuts
the version and starts the run, and the developer supplies real values from a
deep-linkable configuration section on the Builds page — while the coding agent
works, after it finishes, or from a link they send to someone else.

### Readiness is derived, not stored

Secret values cannot be read back from the secret backend, so there is nothing
to store that would not immediately be a second source of truth. The rule
iterates the **design's** current union config schema and looks each key up on
the binding — not the other way round, because the resource pipeline does not
prune, so a key dropped from a design lingers on the binding and must not read
as configured. Per dependency:

- `not-provisioned` — no binding exists;
- `unset` — any currently declared key is absent or empty;
- `configured` — every currently declared key is non-empty.

`configured` is deliberately not *ready*: OpenChoreo's `Ready` is True
throughout, including while every key is `unset`, and reusing the word would
merge two facts that differ.

### The gate sits at the top of the deploy stage

ADR-0017 put the deploy inside the cycle, between builds-green and validation,
so that validation asserts against a version that is genuinely serving. The gate
goes immediately before the ordering:

```text
merge → build fan-out → builds green → [GATE] → wave 0 → … → converge → Ready → validation
```

The gate is a workflow activity calling the readiness service method directly —
not back through HTTP — and it treats its two halves differently:

- a **platform resource** not yet provisioned is the platform still working, so
  it **retries**;
- an **`unset` external dependency** is the user not having acted, so the run
  **parks** in the existing `waiting` state.

Parking on the first would hang on something that resolves itself. Parking on
the second is the entire point. Nothing failed, so the run is not failed: it
stays visibly open, naming the dependencies it is waiting for, and that is what
tells the developer there is something to do.

The park is **outside** `deployReadyTimeout`. That budget bounds how long a
binding may take to serve; a human finding an API key is unbounded by design,
and charging them against the deploy budget would settle `deploy-budget` on a
run that is working exactly as intended.

Waking is a signal carrying a *fact*, not a command, per the existing signal
contract — the supervisor re-derives readiness itself.

### Design faults remain build blockers

An ambiguous or unresolved external dependency, an unresolved external
specification, or an organization service still needing resolution must be fixed
before a version is cut, and sends the developer back to the spec chat where the
design lives. Missing external *values* are deliberately a different fact.

## Consequences

- Developers start coding immediately, and can share or reload the exact
  configuration URL while credentials are gathered.
- The coding agent receives stable environment-variable names, defined and
  empty, and is told in its dispatch context that external credentials are not
  configured and live calls will not succeed. It no longer integration-tests
  against live third-party services.
- Readiness reflects a design-schema or binding change with no second record to
  synchronise.
- OpenChoreo binding `Ready` and AEP value readiness stay separate facts. Code
  must not substitute one for the other.
- A run can now park on a human for an unbounded time. Only cancellation exits
  `waiting`, so the blocking reason is surfaced on the build card with a button
  into the configuration section — an unexplained `waiting` looks like a hang and
  runs pile up behind it.
- An external secret with an empty remote reference sits erroring in the secret
  operator's status between provisioning and configuration. Nothing reads it, but
  it shows red on any operator dashboard.
- The read-then-write merge that preserves existing values across a rebuild is
  not atomic. A save landing in its narrow window is lost; accepted, and the
  merge only ever skips keys that already hold values.
- A legitimately empty value cannot be represented as configured, because the
  schema has no `required` flag. "Empty means unset" makes such a value block
  deploy permanently. Accepted; the real fix is a spec and agent-schema change.
- There is no manual deploy or redeploy endpoint. Deploy is workflow-owned, so
  "the config was wrong, fix it and redeploy without a new milestone" has no
  path today. Noted as a likely follow-up.

## Alternatives rejected

- **Gate the binding's readiness on the secret operator.** The controller
  supports a per-entry ready condition that would hold a binding not-ready until
  its external secret syncs. It would hold every unconfigured dependency's
  binding not-ready and resurrect exactly the build-time blocking this removes.
- **Wait for the secret to land before deploying.** The platform has no read
  path for it — the data-plane proxy client has apply and delete but no get, and
  the binding's Ready condition says nothing about the operator. No wait is
  needed, and that is structural rather than lucky: an empty secret-store path
  produces no Kubernetes secret at all, so at deploy the secret either exists
  with real values or does not exist, and a pod referencing a missing secret
  retries until Kubernetes finds it. The dangerous case — a secret that exists
  holding stale values, so the pod starts happily and never restarts — cannot
  arise. The cost is a few seconds of pod crash-looping.
- **Keep collecting values at build time and merely warn.** Keeps every problem
  in the Context section and adds a warning nobody reads.
- **Store readiness as a column.** Requires a write path on every design edit and
  every value save, and is wrong the moment either happens out of band.

## Verification

Authoring and readiness are covered in
`services/aep-api/internal/dependencies/provisioning`; the gate's decisions in
the milestone run's Temporal test suite (parks on `unset`, retries on a
provisioning resource, wakes and deploys on signal, never deploys a failed or
cancelled run, fails closed on a nil gate); the console in the Builds page tests
over the existing mock handlers.

The first test to write is the regression this change itself creates: **a second
build over a configured binding preserves the real values and does not overwrite
them with empty ones.**

OpenChoreo's own rendering and deployment behaviour is outside the unit-test
boundary and is covered by local-stack evidence: no drawer on Build, the Builds
redirect, every declared key authored empty or seeded with its default, values
saved while the run continues, readiness flipping to `configured`, a rebuild
preserving them, a run parking with a stated reason, and the run waking and
deploying when the last value is saved.
