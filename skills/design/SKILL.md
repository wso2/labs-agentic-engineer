---
name: design
description: Use when generating a project's design from its PRD — the /design flow that turns specs/requirements/prd.md into the cell-first design under specs/design/, then mints the validation criteria. Also the flow for converging an existing design onto an amended PRD.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Design

The design step: derive the complete design of the PRD from
`specs/requirements/prd.md`, cell-first. The design covers EVERY story the
PRD defines. The build gate checks the result mechanically — every story
claimed by some component's design.json, every component enriched — so the
way to a clean Build is to follow the order below.

## The PRD is the brief

Design FROM `specs/requirements/prd.md`. Do not interview the user again and
do not widen or narrow the scope: what the PRD says is what gets designed. A
missing or empty PRD means the user needs `/start` first — stop and say so.

**Open questions gate:** any PRD Open Question neither answered nor marked
"deferred" blocks design — stop and point the user at the amend flow's
resolve-open-questions branch. Deferred questions never block.

## The lineup

Each step names the skill that governs it. Those bodies are inlined for this
turn — apply them directly, and load one only if you find you do not have it.

1. **design.cell** (`cell-design`) — emit the cell FIRST: every component,
   boundaries and edges. The console streams it into the live diagram, and
   the platform scaffolds a design.json skeleton per deployable component
   when it lands.
2. **Component enrichment** (`architecture`) — fill each
   component's design.json: language (org Tech stack default first), the PRD
   `stories` it serves (every story the PRD defines must be claimed by some
   component — the build gate checks coverage), dependencies (discover before
   you invent), description, pinned skills.
3. **design.md** — a DIAGRAM document, mermaid throughout: one Overview
   paragraph, then `## Context (C1)` (a mermaid graph: the PRD's actors, the
   system, external systems), `## Domain model (ER)` (a mermaid erDiagram:
   entities, key fields, relations — these become the API schemas), and
   `## Key flows` (one mermaid sequenceDiagram per core workflow). No
   Components or Interactions prose — the cell owns C2.
4. **security.md** (`security-design`) — when the design has sign-in or roles.
5. **Per-component artifacts** — every `service` gets `openapi.yaml`
   (`openapi-conventions`); every `web-application` gets `wireframes.dsl`
   (`wireframes`).
6. **Validation criteria** (`validation-criteria`) — mint
   `specs/validation/validation-criteria.json` LAST. A design without its
   acceptance oracle is unfinished — never skip this.

Order binds only where a step reads an earlier one's result: the cell before
enrichment (the platform scaffolds each design.json from it), and design.md's
ER model before `openapi.yaml` (those entities become the API schemas).
Everything else is independent — emit independent artifacts as parallel calls
in ONE step, not a step each.

## Regeneration and the delta pass

A design already exists → CONVERGE it to the current PRD: update what
drifted, remove what the PRD no longer calls for, keep what holds.

An amended PRD is a **delta pass with shipped parts protected**: design what
the new stories require and touch shipped components only where those stories
force it — calling out every such change. When built reality contradicts the
design, surface the conflict to the user; never silently redraw shipped
architecture.

## Where this stops

`/design` ends at the design and its validation criteria — no task planning,
no application code. Close with three parts and nothing more: one line per
component (name, type, one-clause role); a **"Needs your input"** block
listing only the dependencies still ambiguous or unresolved; and a one-line
pointer to `specs/design/`. The dependency narration during the turn (the
`architecture` skill owns its format) already carried the play-by-play.
