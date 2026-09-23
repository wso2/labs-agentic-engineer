# ADR-0034 — A web application is reviewed as a prototype after design, alongside its wireframes

**Status:** Accepted · 2026-09-23
**Related:** [ADR-0025](ADR-0025-a-web-application-is-verified-before-it-is-committed.md)
(the mock walk still reads the wireframes as its map),
[console ADR-0007](../../apps/console/design/decisions/ADR-0007-design-gate-is-build-trigger.md)
(Build is the approval),
[console ADR-0008](../../apps/console/design/decisions/ADR-0008-design-views-derived-client-side.md)
(the wireframe canvas),
[console ADR-0033](../../apps/console/design/decisions/ADR-0033-preview-and-annotate-are-one-prototype-view.md)
(how the prototype is reviewed). Feature:
[#813](https://github.com/wso2/labs-agentic-engineer/issues/813).

## Context

`/design` draws each `web-application` as `wireframes.dsl`, a line-oriented
Excalidraw dialect the console compiles into a canvas and a click-through view.
The wireframes are the coding run's screen contract and the map the mock walk
follows, and they stay so. As a picture to *review*, though, they fall short:

* **A sketch, not an application.** Grayscale boxes laid out by a compiler,
  one role at a time. A reviewer cannot walk it as a role through a flow, see
  an error state, or tell whether a table would hold what the API returns.
* **Feedback is prose.** The only way to correct a screen is to describe it in
  chat and hope the agent maps the words back to the right line of the DSL.

## Decisions

1. **The prototype is an additional review step after Design.** Stage order is
   Requirements → Design → Prototype → Validation. The prototype is derived
   *from* the finished design: the cell names the web applications,
   `security.json` names the roles, each web application's API (its own and its
   component dependencies' `openapi.yaml`) shapes the records. The Prototype
   stage exists only when the design cell declares at least one
   `web-application`, and the rail derives that from the cell every time — no
   stored flag. `/design` still writes `wireframes.dsl`, and nothing about the
   wireframes changes.

2. **One `prototype.json` per web application**, at
   `specs/design/components/<component>/prototype.json`, beside the component's
   `wireframes.dsl`. It is written by the `/prototype` flow (the `prototype`
   skill with the organization's design-system skill), never by `/design`, and
   generation is a user click (**Generate** in the Spec rail's Prototype
   header), never platform-fired. The design tree hides it as a file row (it has
   its own Prototype entry) and the design projection publishes it as the
   component's `prototype` artifact, next to `wireframes`.

3. **The model is a versioned, controlled registry** (`@aep/prototype-model`,
   schema version 1): layouts, navigation, content, forms, data, workflow nodes
   and two overlays, and seven actions that change view state only. There is no
   markup, style, script, custom component, chart or upload escape hatch, so one
   renderer draws any prototype and a generated file cannot execute anything.
   Every screen, flow, state, role and node carries a stable ID; labels are never
   identities. Mock records live in the file and are deterministic; the renderer
   never generates data.

4. **One definition, three readers.** The package owns the Zod schema, the
   reference pass (one global ID namespace; duplicates rejected before targets
   resolve) and the deterministic serializer. The agent's write gate uses it
   directly; the console parses with it; the Go save gate validates against its
   generated JSON Schema, vendored into `platform/prototypespec` behind an
   anti-drift test, and reports the same codes at the same paths. Any present
   `prototype.json` is validated on save, a blank one included. A malformed or
   unsupported file renders nothing rather than part of itself.

5. **Build neither requires nor approves the prototype.** The build gate keeps
   asking a web application for its `wireframes.dsl` and nothing more; a project
   that never generates a prototype builds as before. There is no approve
   operation, approval record, history or rollback: publishing a version stays
   the one moment of commitment (console ADR-0007). A prototype present at the
   tag still passes the design-bundle validation Build runs, so an invalid one
   cannot ride into a version. `prototypeOutdated` is derived, like
   `designOutdated`, from the design fingerprint (the design tree minus
   prototype files) at the prototype's last commit; a feedback rewrite of the
   prototype alone never marks anything outdated.

6. **The coding run does not read the prototype.** `architecture` pins
   `wireframes` on every `web-application` (with `react-webapp` and the
   organization's design system), and the coding run builds its screens from
   `wireframes.dsl`. The `prototype` skill's audience is the design side only.
   What a reviewer settles on the prototype reaches the build through the
   design: a request that needs a design change says so, and the design turn
   that makes it rewrites the wireframes too.

## Alternatives considered

**Replace the wireframes with the prototype.** Tried first: the prototype took
every slot the DSL held — the build gate's mandated artifact, the coding run's
screen contract, the mock walk's map — and the DSL was removed. It made the
optional review a prerequisite of every build and moved the coding contract onto
a model that was days old. Keeping the wireframes and adding the prototype beside
them gets the review without either cost.

**A prototype before design, app-wide.** It would have needed its own approval
endpoint, its own read endpoint and an app-level artifact, and it would have
invented roles and records the design then had to agree with. Running after
design removed all three.

**Add annotation to the wireframe canvas.** Feedback would still land on a
sketch whose layout a compiler owns, and a comment anchored to a drawn box has
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
* A project whose design has no web application never sees the stage, and no
  project is gated on it.
* A web application has two pictures that can disagree: the wireframes the build
  follows and the prototype the reviewer walked. The prototype is regenerated
  from the design, so a review that changes the design converges them; a
  prototype-only feedback rewrite does not touch the wireframes.
* Adding a node kind is a schema version decision: the model, the JSON Schema,
  the Go vendor copy, the renderer registry and the skill's registry table move
  together, and a test or a type check in each place fails until they do.
