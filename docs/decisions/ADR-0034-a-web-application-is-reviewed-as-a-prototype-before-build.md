# ADR-0034 — A web application is reviewed as a prototype after design, and Build requires it

**Status:** Accepted · 2026-09-23
**Amends:** [ADR-0033](ADR-0033-screen-gates-derive-from-operations.md) point 4
(a public screen is no longer "a flow with no `role` line"; see decision 6).
**Related:** [ADR-0025](ADR-0025-a-web-application-is-verified-before-it-is-committed.md)
(the walk now reads the prototype as its map),
[console ADR-0007](../../apps/console/design/decisions/ADR-0007-design-gate-is-build-trigger.md)
(Build is the approval),
[console ADR-0033](../../apps/console/design/decisions/ADR-0033-preview-and-annotate-are-one-prototype-view.md)
(how the prototype is reviewed). Feature:
[#813](https://github.com/wso2/labs-agentic-engineer/issues/813).

## Context

`/design` used to end by drawing each `web-application` as `wireframes.dsl`, a
line-oriented Excalidraw dialect the console compiled into a canvas and a
click-through view. It was the only picture of the application before Build,
and it was the wrong one to review:

* **A sketch, not an application.** Grayscale boxes laid out by a compiler,
  one role at a time. A reviewer could not walk it as a role through a flow, see
  an error state, or tell whether a table would hold what the API returns.
* **Feedback was prose.** The only way to correct a screen was to describe it in
  chat and hope the agent mapped the words back to the right line of the DSL.
* **It was drawn before the facts it depended on.** The screens were written in
  the same turn as `security.json` and each component's `openapi.yaml`, so their
  roles and records were guesses the rest of the design could later contradict,
  and nothing reported the drift.
* **Build shipped it unreviewed.** The build gate required the DSL to exist and
  nothing else.

## Decisions

1. **Stage order is Requirements → Design → Prototype → Validation.** The
   prototype is derived *from* the design: the cell names the web applications,
   `security.json` names the roles, each web application's API (its own and its
   component dependencies' `openapi.yaml`) shapes the records. The Prototype
   stage exists only when the design cell declares at least one
   `web-application`, and the rail derives that from the cell every time — no
   stored flag.

2. **One `prototype.json` per web application**, at
   `specs/design/components/<component>/prototype.json`, the 1:1 successor of
   `wireframes.dsl` in every slot the DSL held: the design tree, the design
   projection's per-component artifact, the build gate's mandated artifact, the
   pinned skill that carries it into the coding run, and the map the mock walk
   follows. It is written by the `/prototype` flow (the `prototype` skill with
   the organization's design-system skill), never by `/design`, and generation
   is a user click (**Generate** in the Spec rail's Prototype header), never
   platform-fired.

3. **The model is a versioned, controlled registry** (`@aep/prototype-model`,
   schema version 1): layouts, navigation, content, forms, data, workflow nodes
   and two overlays, and seven actions that change view state only. There is no
   markup, style, script, custom component, chart or upload escape hatch, so one
   renderer draws any prototype and a generated file cannot execute anything.
   Every screen, flow, state, role and node carries a stable ID; labels are never
   identities. Mock records live in the file and are deterministic; the renderer
   never generates data.

4. **One definition, three gates.** The package owns the Zod schema, the
   reference pass (one global ID namespace; duplicates rejected before targets
   resolve) and the deterministic serializer. The agent's write gate uses it
   directly; the console parses with it; the Go save gate validates against its
   generated JSON Schema, vendored into `platform/prototypespec` behind an
   anti-drift test, and reports the same codes at the same paths. A malformed or
   unsupported file renders nothing rather than part of itself.

5. **Build is the approval, and it requires the prototype.** There is no approve
   operation, approval record, history or rollback: publishing a version stays
   the one moment of commitment (console ADR-0007). Build refuses a web
   application without a prototype (`MISSING_COMPONENT_ARTIFACT`), one whose
   prototype is invalid (the design-bundle validation Build runs first, with the
   save gate's per-finding codes) and one whose roles `security.json` does not
   declare (`UNKNOWN_PROTOTYPE_ROLE`) — the roles the user reviewed are the roles
   the build creates. The structural save gate checks shape only; the role
   cross-check needs the whole bundle, so it lives with the other cross-file
   rules at Build. `prototypeOutdated` is derived, like `designOutdated`, from
   the design fingerprint (the design tree minus prototype files) at the
   prototype's last commit; a feedback rewrite of the prototype alone never marks
   anything outdated.

6. **The coding run reads the prototype as its screen contract.** `architecture`
   pins `prototype` on every `web-application` (with `react-webapp` and the
   organization's design system), and the skill's coding reference says how
   screens, nodes and actions become routes, elements and navigation; its mock
   records seed mock mode. Every prototype screen and flow names a declared role,
   so a screen shown before sign-in is one the PRD gives to a signed-out visitor,
   not one inferred from a flow without a role.

7. **Cutover without a converter.** Once the JSON path was proven end to end,
   the DSL write gate, the wireframes skill, the Excalidraw prototype view and its
   derivation, the Wireframe rail row and the design flow's wireframes step were
   removed. No converter, no coexistence layer, and no reading of an old
   `wireframes.dsl` as input: a project designed before the cutover generates its
   prototype like any other. `@aep/excalidraw-dsl` keeps only its domain-model
   dialect and keeps its name.

## Alternatives considered

**A prototype before design, app-wide.** The first scoping. It would have needed
its own approval endpoint, its own read endpoint and an app-level artifact, and
it would have invented roles and records the design then had to agree with.
Running after design removed all three.

**Keep the DSL and add annotation to the canvas.** Feedback would still land on
a sketch whose layout a compiler owns, and a comment anchored to a drawn box has
no stable identity to survive the next regeneration.

**Generated React per application.** Maximally faithful and unreviewable as
data: nothing could validate it, diff it or refuse it, and it would be an
executable escape hatch inside the console.

**An explicit approve step.** A second commitment beside Build, a stored SHA to
keep in step with the file, and a gate that asks whether a human looked rather
than whether the artifact is valid. Console ADR-0007 already rejected that
shape for the design.

## Consequences

* A reviewer walks each web application as each role, through each flow, in each
  display state, and points at components instead of describing them. The
  console side is console ADR-0033.
* A project whose design has no web application never sees the stage and is
  never gated on it.
* Adding a node kind is a schema version decision: the model, the JSON Schema,
  the Go vendor copy, the renderer registry and the skill's registry table move
  together, and a test or a type check in each place fails until they do.
