---
name: design
description: Use when generating a project's design from its PRD — the /design flow that turns the requirements under specs/requirements/ into the cell-first design under specs/design/, then mints the validation criteria. Also the flow for converging an existing design onto an amended PRD.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Design

The design step: derive the complete design of the PRD from the requirements
under `specs/requirements/` — the product page `prd.md`, one file per feature
under `features/`, and `product-wide.md` (`prd-contract`) — cell-first. The
build gate checks the result mechanically — every story claimed by some
component's design.json, every component enriched — so the way to a clean
Build is to follow the order below.

## Which features this run designs

Design works per feature. A feature is **designable** once it has been
interviewed (it has stories) and no `*blocking*` question in its Open
Questions stops it; a stub and a blocked feature are left out, and say so in a
line.

- `/design F1 F2` names the features: design those, and leave every other
  feature's part of the design as it is — shared components change only where
  the named features need them to.
- A bare `/design` designs every designable feature.
- Lines tagged `*assumed*` are designed **as written**. Seeing an assumption in
  a flow or a screen is the quickest way for the user to judge it; do not
  stop to ask about one, and do not leave it out.

The build checks each feature against the requirements the run that designed
it read: a feature whose words change after its design is out of date until a
run designs it again, and every other feature's design stands.

## The PRD is the brief

Design FROM those files, and do not widen or narrow the scope: what the PRD
says is what gets designed. No `prd.md` means the user needs `/start` first,
and no designable feature means a feature's interview comes first
(`/interview F<n>`) — stop and say so.

**Ask at design altitude.** A call this step has to make and only the user can
settle — which provider, which of two shapes the PRD deliberately left open —
is an ordinary question, asked when it arises rather than assumed silently or
deferred to a review that never happens. `grilling` carries the mechanics and
the pacing. The PRD's own answers are settled: asking one back reads as the
document being ignored.

**Open questions never block design.** They are recorded gaps, not corruption:
design what the PRD does say, and where one genuinely decides a call you are
about to make, ask it as an ordinary question — the same way you ask anything
else at design altitude. An entry marked "deferred" is one the user has already
declined for now; leave it alone.

## Reference documents ground the design

The kickoff may have attached reference documents — and for design, the ones
that matter most are the user's own sketches: a drawn wireframe, a form
screenshot, a mockup image. They are attached to this conversation natively
(images and PDFs) or in your workspace files (text). When any exist:

- **A user-drawn wireframe sketch is the layout brief.** `wireframes.dsl`
  follows what the user drew — screen structure, navigation, the controls
  they placed — refined, not reinvented. Look at the image before writing a
  single screen.
- A form document (paper form, PDF) is the field inventory: the screens that
  digitize it carry its fields and sections.
- Where a sketch and the PRD disagree, the PRD's scope wins, but the sketch's
  layout intent survives inside that scope — and the discrepancy is worth a
  line in the design notes.

No documents attached is the ordinary case: design from the PRD alone.

## Say what you are about to write

Design runs long, and a reader who can only see finished files cannot tell how
much is left. **Call `declare_plan` before you start writing**, naming the
files that step is about to produce, and call it again each time the plan grows
— you cannot know the per-component files until the cell fixes the component
set, so the list arriving in waves is the real shape of the work, not a failure
to plan. Restating a path you already declared is harmless.

It does not end your turn: declare, then write. The declaration and the
artifacts appearing as you write them are what keep the user informed — you do
not need to narrate your progress alongside them.

## The lineup

Each step names the skill that governs it. Those bodies are inlined for this
turn — apply them directly, and load one only if you find you do not have it.

0. **Declare the first wave** — `declare_plan` with what you can already name:
   `specs/design/design.cell` and `specs/design/domain-model.md` at minimum,
   plus each `specs/design/flows/<slug>.md` as soon as you can name the flow.
1. **design.cell** (`cell-design`) — emit the cell FIRST: every component,
   boundaries and edges. The console streams it into the live diagram, and
   the platform scaffolds a design.json skeleton per deployable component
   when it lands.
2. **Component enrichment** (`architecture`) — the component set now exists, so
   `declare_plan` the per-component files before writing them. Fill each
   component's design.json: language (org Tech stack default first), the
   `stories` it serves, by ID (`"F2.3"`; every story the feature files define
   must be claimed by some component — the build gate checks coverage), dependencies (discover before
   you invent), description, pinned skills. A dependency is a cell node: a
   database or cache you introduce here goes into design.cell first
   (`component <id> as "…" database`, inside the cell) — the cell is the
   source of truth, and a design.json naming a node it lacks is refused. An
   external dependency is ALSO its own file, written before the component
   that references it: `specs/design/dependencies/<name>/dependency.json`
   plus the contract slice beside it (`architecture` owns the shape).
3. **domain-model.md** — `specs/design/domain-model.md`: an H1 title, one
   or two sentences of intro, then exactly ONE mermaid `erDiagram` (entities,
   key fields, relations — these become the API schemas). Brief entity notes
   after the diagram are fine; keep them to a few lines. Never a second
   erDiagram — the API schemas derive from this one.
4. **Key flows** — one file per flow: `specs/design/flows/<kebab-slug>.md`,
   an H1 title, one or two sentences naming the actor and the outcome, then
   exactly ONE mermaid `sequenceDiagram`. A key flow is a PRD actor's
   end-to-end journey: it starts with an actor, spans cell components, and
   involves more than one component interaction or a decision/async step —
   plain CRUD on one entity is NOT a flow. Every participant must be a node
   design.cell declares (a component, or a boundary external such as the
   identity server or a SaaS) or an actor from the PRD — never an invented
   name. No context/C1 diagram anywhere: the cell and the PRD carry that.
   The shape is formulaic — write it like this, first try:

   ```mermaid
   sequenceDiagram
       actor Employee
       actor LineManager as Line Manager
       participant expense-webapp
       participant expense-api

       Employee->>expense-webapp: submit claim (amount, receipt)
       expense-webapp->>expense-api: create claim
       alt no receipt
           expense-api-->>expense-webapp: refused
       else
           expense-api-->>expense-webapp: created
       end
       LineManager->>expense-webapp: approve
   ```

   Names are ONE word. A multi-word PRD actor gets an alias — `actor
   LineManager as Line Manager` — and every message uses the one-word id;
   spaces in a declared name or a message endpoint are refused. The
   platform judges both documents as you write them: a second diagram,
   a statement outside plain mermaid, or an unresolved participant is
   refused (`INVALID_DIAGRAM`, `UNKNOWN_PARTICIPANT`) with the offending
   line and the ids you may use — fix it and re-emit the whole file once.
5. **Security design** (`security-design`) — `specs/design/security.json` when
   the design has sign-in or roles.
6. **Per-component artifacts** — every `service` gets `openapi.yaml`
   (`openapi-conventions`); every `web-application` gets `wireframes.dsl`
   (`wireframes`); every `ai-agent` gets `agent.afm.md` (`agent-building`).
7. **Grants pass** (`security-design`) — re-read `specs/design/security.json`
   now that the screens and the operations exist. Step 5 wrote each role's
   `grants` against a design it could only intend; the operations the screens
   in each role's flow load are decidable only here. Walk each flow, open the
   contract behind each screen, and make sure the role holds the handle of the
   operation each screen loads. Make sure also that each field the screen
   draws (a column, a value, a name in a select or a list) comes from that
   operation, or from one list operation that each role loading the screen
   may call. A field that no operation gives is a gap in the contract, and the
   build cannot change the contract: add the field now to the response of the
   operation that the screen loads (a name next to its id). Do not widen a
   scope to close the gap. The service fills a name from its own records,
   never from a name that the client sends. Re-emit a file only if it
   changes. Skip
   the step only when step 5 wrote no security.json at all. No gate refuses a
   role that is one handle short — the build's mock walk is what catches it, as
   a hidden screen — so this pass is where it is cheap.
8. **The acceptance oracle** — mint it LAST. A design without one is
   unfinished — never skip this.
   - `acceptance-criteria` → `specs/validation/acceptance/F<n>-<slug>.feature`,
     one per feature this run designs.

   Authored from the requirements alone, so it is independent of everything
   above.

Order binds only where a step reads an earlier one's result: the cell before
enrichment (the platform scaffolds each design.json from it),
domain-model.md's ER model before `openapi.yaml` (those entities become the
API schemas), and the per-component artifacts before the security
reconciliation (it is those files it reconciles against).
Everything else is independent — emit independent artifacts as parallel calls
in ONE step, not a step each.

## Regeneration and the delta pass

A design already exists → CONVERGE it to the current PRD, for the features
this run designs: update what
drifted, remove what the PRD no longer calls for, keep what holds. A legacy
`specs/design/design.md` (the retired single-file overview) is not part of
the design any more — `removeFile` it and put its content where it now
belongs (domain-model.md, flows/).

An amended PRD is a **delta pass with shipped parts protected**: design what
the new stories require and touch shipped components only where those stories
force it — calling out every such change. When built reality contradicts the
design, surface the conflict to the user; never silently redraw shipped
architecture.

## Where this stops

`/design` ends at the design and its acceptance criteria — no task planning,
no application code. Close with three parts and nothing more: one line per
component (name, type, one-clause role); a **"Needs your input"** block
listing only the dependencies still unresolved, each as a link to its
definition (`[<name>](aep://spec/specs/design/dependencies/<name>/dependency.json)`,
the `architecture` skill's closing form) followed by the one thing you need,
so the user opens it with a click; and
a one-line pointer to `specs/design/`. The dependency narration during the
turn (the `architecture` skill owns its format) already carried the
play-by-play.
