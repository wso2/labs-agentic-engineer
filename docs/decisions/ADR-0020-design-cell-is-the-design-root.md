# ADR-0020 — design.cell is the design root; design diagrams are one per file

**Status:** Accepted

## Context

The design bundle's root used to be `specs/design/design.md` — the "Design
overview": one markdown file holding an overview paragraph, a C1 context
diagram, the ER domain model, and every key flow as stacked mermaid blocks.
Three problems accumulated:

1. **The root was prose, but the authority was the cell.** `design.cell`
   already declared the components (the scaffold engine and the build gate
   read it — it is the primary design source), while `design.md` restated a
   thin overview the PRD also carries. Two roots, one real.
2. **A multi-diagram document has no unit of validation.** "Is this file
   valid?" has no crisp answer when one file holds four diagrams; a generated
   diagram could not be checked (or regenerated) in isolation.
3. **The C1 diagram was pure duplication.** Its actors and external systems
   are the PRD's; its structure is the cell's. Any disagreement between C1
   and the cell was a bug with two plausible truths.

## Decision

- **`specs/design/design.cell` is the design root.** Its presence is the
  design-exists marker (assemble, save gate, build gate, playground tasks
  gate, eval checks), it is the protected path agents cannot delete, and it
  carries the optional `sourceSpec` YAML frontmatter — a leading `---` block
  both cell parsers (Go fact extraction, TS grammar) skip.
- **`design.md` is gone.** No legacy read path: a bundle without a cell reads
  as "no design yet". The overview prose it held lives in the PRD.
- **The C1 context diagram is dropped.** The cell diagram is the structural
  source of truth; the PRD names the actors and external systems.
- **One diagram per file.** The ER model lives in
  `specs/design/domain-model.md` (title, one–two sentence intro, exactly one
  `erDiagram`; its entities become the OpenAPI schemas). Each key flow lives
  in `specs/design/flows/<kebab-slug>.md` (title, one–two sentences naming
  actor and outcome, exactly one `sequenceDiagram`). A key flow is a PRD
  actor's end-to-end journey across cell components — plain CRUD on one
  entity never qualifies — and every participant must resolve to a node the
  cell declares (a component or a boundary external) or a PRD actor.

## Consequences

- One file = one diagram = one checkable unit: mermaid syntax and
  participant-resolution checks can pass or fail a single file, and the
  design agent can regenerate exactly the diagram that failed.
- `DesignFile` assembles structured facts only (components + the root
  frontmatter); it no longer carries overview prose. `SplitDesign` renders
  per-component files only — the cell is authored, never rendered.
- The console rail can present Domain model and Flows as first-class rows
  (follow-up: #686); generation-time validation of the per-file contracts is
  a follow-up (#687).
- Existing bundles with a `design.md` and no `design.cell` read as "design
  missing" until the design is regenerated — a deliberate clean break.
- The astryx design-system skill's per-project `## Brand colors` override
  moves from `design.md` to the PRD (`specs/requirements/prd.md`) — the
  document that already records what a person stated outright.
