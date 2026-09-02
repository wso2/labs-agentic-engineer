# ADR-0020 — Onboarding imports requirements only

**Status:** Accepted

## Context

Onboarding an existing application into AEP looks like a second pipeline if the
platform clones the foreign repo, analyses it on a runner, vendors code
(`importAsIs`), and threads a `modernizes` / parity cutover through task
planning. That path has three structural problems:

1. **Extraction is an interview against a running system.** Load-bearing versus
   accidental behaviour, seed data, secrets, and the person who knows the
   product all live on the developer's machine. A pod cannot ask those
   questions.
2. **Vendored code skips the platform's wiring.** An opaque blob never
   participates in declarative dependency wiring (ADR-0004), metadata-driven
   resource consumption (ADR-0007), or auth as a platform resource (ADR-0006).
3. **If extraction emits a cell, the agent can reproduce the legacy folder
   structure as components.** Mitigations are brittle; the reliable fix is to
   never record structure.

## Decision

**The front door for existing applications is a requirements import.**

1. A developer runs the `modernize-extract` skill in the **legacy repository**
   (Claude Code / Cursor), producing a flat requirements bundle (`prd.md` plus
   domain model, business rules, integrations).
2. They upload that tarball into a **new empty AEP project** via
   `POST /projects/{projectName}/requirements/import`. The endpoint is
   create-only, reuses `parsePRDStories` so it refuses what Build would refuse,
   commits under `specs/requirements/`, and cuts a requirements `vN` tag.
3. From `/design` onward the project is an ordinary AEP project. The design
   skill honors the whole requirements corpus beside `prd.md`.

AEP never clones the legacy repository, never runs an analysis execution kind
for onboarding, and never vendors application source.

## Consequences

- `sourceMode`, `importAsIs`, the `modernizes` link, and parity cutover are out
  of scope rather than threaded through task planning.
- Size of the imported markdown is a correctness concern: every `.md` is
  inlined into every design turn with no truncation — the importer soft-warns
  above 64 KiB and hard-refuses above 256 KiB.
- Install of `modernize-extract` onto a developer's machine is a copy from the
  org's `org-skills` library (or the console skill viewer). The skill refuses
  to run inside an AEP project tree.

## Rejected alternatives

- Foreign-repo clone + analysis on the platform
- `importAsIs` vendoring with synthesized Dockerfiles
- Parity cutover between legacy and modernize siblings
- A headless CLI push that bypasses the requirements save gate
