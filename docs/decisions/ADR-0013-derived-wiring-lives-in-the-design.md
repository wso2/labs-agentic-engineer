# ADR-0013 — Derived wiring lives in the design; only resolved wiring is pushed

[ADR-0004][adr4] established that the coding agent authors every line of a
component's `workload.yaml`, so the platform must SAY where a dependency lives
rather than patch it, and it said so by posting a "Platform-resolved dependencies"
comment when the dependency's provisioning gate resolved.

That trigger has a snapshot audience: the project's open agent-work issues at the
instant the gate closes. The plan path (`fillMilestone`) provisions **before** it
plans, so on a project's first build the gates resolved ~20 seconds before the
implementation issues existed, the comment had nobody to post to, and nothing ever
re-drove it. The agent then behaved exactly as the skill instructed — "no block for
a component means it has no consumer-side dependencies" — and shipped SQLite on a
container filesystem for a component whose design declared `postgres-cnpg`.

The deeper problem was not the ordering. It was that **the `resources:` half never
needed the gate at all**: a resource's OC ref and its output→env-var mapping are
pure functions of the design plus the resource type's DECLARED outputs. Nothing
about them requires a binding to exist. The platform was paying an ordering cost
for information it already had at design time.

## Decision

Split the block by what is DERIVED from what is RESOLVED, and give each the
channel its nature allows.

**Derived — stamped into `design.json`.** Every `platform-resource`, `external`
and `component` dependency carries a platform-stamped `wiring` object,
shape-identical to the entry it belongs in so the agent copies rather than
transforms it: `{ref, envBindings}` for one `dependencies.resources[]` entry, and
`{endpoint}` for one `dependencies.endpoints[]` entry. A **sibling**'s endpoint
qualifies for the same reason a resource's ref does — scoped provider name, the
sibling's own endpoint name, `project` visibility and one `address` binding are
all pure functions of the design. It is derived in the design-save pass (the
pre-tag step on `POST /build`), committed to the design the agent reads as its
spec, and captured in the version tag. There is no ordering, no audience, and no
idempotency marker left to get wrong.

**Resolved — pushed at cycle dispatch.** What is left on the `endpoints:` half is
an `org-service`, which genuinely needs live resolution because its provider
belongs to another project and may not have published yet, so it stays a
comment — but triggered at dispatch, not at gate resolution. Dispatch is provably
correct rather than lucky: the dispatch predicate already requires that no gate is
open in the milestone, so everything resolvable has resolved, and the working set
is non-empty by construction. It also reaches an issue adopted mid-run, which the
gate trigger never could.

Three properties make this hold:

- **Re-derived and overwritten every pass.** `wiring` is derived data in an
  authored artifact, so a dependency rename or a resource type gaining an output
  must recompute it. Overwriting is also what lets both write gates ACCEPT the
  field instead of rejecting it as agent-authored: the design agent reads, edits
  and writes `design.json` back, so a rejection rule would reject its own echo.
  A malformed wiring still rejects — a half-stamped one renders an unusable
  workload entry, which is worse than an absent one the agent reports.
- **The idempotency marker records COMPLETENESS, not identity.** `aep:wired` is
  stamped only when nothing was omitted from the endpoint block; a partial post
  goes up unlabelled so the next dispatch supersedes it. A first partial answer
  being treated as final was the original bug in miniature.
- **Absence is loud.** A declared dependency with no `wiring` is a platform fault
  the agent reports and stops on — never a licence to substitute its own database,
  cache or IDP. Independently, the merged-PR fan-out compares each component's
  declared refs AND endpoint targets against the `workload.yaml` it shipped and
  mints a fix issue on a mismatch, because the silence was a separate defect from
  the delivery bug: a component that quietly substituted its own persistence
  BUILDS, and one that named a sibling by its unscoped name builds, deploys and
  serves, so nothing else would ever notice.

## Consequences

- **A `design.json` schema change is a multi-image deploy.** Two running
  containers embed the strict schema: `aep-api` (the fold gate) and `aep-agents`
  (every design.json write passes `FileBundle.commit` → `checkComponentDesign`).
  Shipping only `aep-api` leaves the first re-generation over a stamped design
  failing `SCHEMA_VIOLATION`.
- **A stamped `ref` always points at a Ready binding, and the dispatch predicate
  is what guarantees it.** The agent now writes a resource entry unconditionally
  rather than only for resolved dependencies, which would be a hazard if it could
  reference a resource that does not exist yet. It cannot: a provision gate closes
  exactly when its binding reports Ready, and no coding cycle dispatches while a
  gate is open. A design that gains a resource dependency later gets a fresh gate
  on its next build, so the invariant holds across versions too.
- **`wiring` must round-trip through the on-disk codec, in both directions.**
  `design.json` is written by an explicit codec (`dependencyJSON`) whose purpose is
  that derived keys cannot leak into the file — `status`/`reason` are deliberately
  absent from it. `wiring` is the exception that IS persisted, so it has to be
  added there too. This shipped broken once: the field was in the model and all
  three gates but not the codec, so every stamp was silently discarded on write and
  a build produced a design.json with `exposesAPI.auth` set and no wiring at all.
  Dropping it on READ is subtler — the derivation would see no prior value, so the
  change detection would report a diff and commit on every save forever.
- **The naming convention moved to the kernel** (`internal/platform/ocname`).
  `spec` now derives the same ref and env-var names `dependencies` injects, and it
  cannot import `dependencies` — `dependencies` already imports `spec` — so a
  shared home is the only way to keep one source of truth rather than two
  conventions that drift. The agreement is load-bearing and tested: an overflowing
  name must hash-bound to the same stem on both sides or the agent's
  `workload.yaml` references a resource that does not exist.
- **`design.json` now mixes authored and derived fields.** `exposesAPI.auth`
  already set that precedent ([ADR-0007][adr7]); `wiring` follows it. The cost is
  that a reader cannot tell by shape alone which fields the architect chose — the
  architect skill says not to author `wiring`, and the derivation overwrites it
  either way.
- **The stamp is not instant.** It lands on the pre-tag step of `POST /build`, not
  when a draft design is saved, because `SaveAndProceed` was retired and tagging is
  the single-tag build flow. That is early enough for every consumer (the tag, the
  plan, the dispatch) but it does mean a saved-but-unbuilt design shows no wiring.
- **Every output of a web-app's resource dependency stays browser-visible**
  ([ADR-0007][adr7]). With `wiring` in the design that hazard becomes greppable —
  a `web-app` carrying a `*_PASSWORD` binding is now visible at design review
  rather than only in PE guidance. Making it a design-gate rule is a separate
  change.

## Alternatives considered

- **Re-drive the comment after planning.** The cheapest fix, and it does close the
  first-build case. Rejected as the primary fix: it patches one ordering while
  keeping push semantics, so an issue adopted mid-run still gets nothing.

  ADR-0004 had in fact already named the dispatch fix — *"closing it means the run
  supervisor asking for the already-resolved wiring once the plan exists, at the
  cycle's first dispatch"* — but scoped it to dependencies resolving
  SYNCHRONOUSLY in the build step, on the assumption that async resolutions "land
  after the plan and are unaffected". A resource type with static outputs goes
  Ready in seconds and beats an LLM planning turn, so the async path landed in the
  same empty working set. The hazard was documented and the mitigation was named;
  what was missing was that "async, therefore later" is not a guarantee.
- **Carry the block in the dispatch prompt.** Rejected — `buildPrompt` is
  deliberately a milestone reference and nothing else, so the workflow versions
  with the skill rather than with the BFF binary. A rendered data section would
  re-couple the agent's contract to the binary.
- **An MCP `get_resolved_dependencies` tool.** Attractive as a self-serve read,
  but the MCP server is registered only when both URL and token are present and
  silently falls back otherwise; a dependency channel must not have a
  silent-absence mode. Viable later on top of the design stamp, not as the channel.
- **The platform commits the `dependencies:` block into `workload.yaml` itself.**
  Rejected — it contradicts [ADR-0004][adr4]'s decision that the repo is the single
  source of truth for the workload, and it would race the agent's own commits on
  its branch.

[adr4]: ADR-0004-declarative-dependency-wiring.md
[adr7]: ADR-0007-metadata-driven-resource-consumption.md
