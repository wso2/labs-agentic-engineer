# ADR-0008: Design-phase views are derived client-side on demand, read-only, never committed

- **Status:** Accepted — cell-diagram source amended 2026-07-22 (see note below)
- **Date:** 2026-07-09 (rich design view feature,
  [#149](https://github.com/wso2/labs-agentic-engineer/issues/149))
- **Context:** the spec view rendered every design file as raw text (#80
  scoped the renderers out). The rich design view adds a whole-architecture
  cell diagram, per-component wireframes, and a Swagger-style API Spec view.
  Each is a *view* of an authored source — the cell diagram of the component
  `design.json` files, a wireframe of a `wireframes.dsl`, the API Spec of an
  `openapi.yaml`. We had to decide where those views are produced (client vs
  a BE/agent step), whether their output is persisted, and whether they are
  editable. A committed-artifact approach (agents write `.excalidraw` /
  `cell-diagram.gen.json` alongside the sources, the console reads them) was
  considered and rejected.

## Decision

Design-phase views are **derived on the client, on demand, from the authored
sources**, and rendered **read-only**:

- The cell diagram renders the authored `specs/design/design.cell` DSL
  directly; a wireframe is compiled from its `wireframes.dsl` via
  `@aep/excalidraw-dsl`; the API Spec is parsed from `openapi.yaml`. All
  three run in the browser at render time over content already served by the
  Files API — **no new endpoints, no BE/agent derivation step.**

> **Amendment (2026-07-22, [#209](https://github.com/wso2/labs-agentic-engineer/issues/209)):**
> the cell diagram originally *projected* the component `design.json` files
> into the cell DSL via `@aep/design-projection`. Since design.cell became an
> authored, agent-maintained artifact (written first on every architectural
> change), the projection fallback was removed: the diagram reads design.cell
> itself — the live collab doc when connected, the committed blob otherwise.
> The Architecture tab therefore requires design.cell to exist. The ADR's
> principle is unchanged; only the cell diagram's authored source moved from
> "design.json bundle, projected" to "design.cell, rendered as-is".
- **Nothing derived is committed.** There are no `.excalidraw` /
  `*.gen.json` files in the repo; the `design.json` / `.dsl` / `openapi.yaml`
  sources remain the single source of truth.
- The views are **read-only** — no canvas editing or persistence. Editing
  happens on the source (as text) where it happens at all.
- The rendering components are shared workspace packages under `packages/ui/`
  (`@aep/ui-cell-diagram-view`, `@aep/ui-excalidraw-view`,
  `@aep/ui-openapi-view`), built with Oxygen UI, independent of
  `console-legacy`.

## Consequences

- Future design-phase visualizations follow this shape: derive in the browser
  from the authored source, render read-only, commit nothing. A feature that
  needs a *persisted* or *editable* artifact must supersede this ADR
  explicitly and add the BE contract for it.
- The Contract changes for such views are **None** — they read existing Files
  API content, so no BE handshake is required.
- A malformed source degrades gracefully rather than failing the view (a bad
  `design.json` projects to a default component; a `.dsl` that doesn't compile
  shows a "could not be rendered" message; an unparseable `openapi.yaml` shows
  a parse error) — never a crash or blank pane.
- No commit/write path exists for these views, so there is no save state,
  dirty tracking, or conflict handling to design for them.
