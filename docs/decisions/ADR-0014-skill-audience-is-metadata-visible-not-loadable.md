# ADR-0014 — Skill audience is declared metadata; coding skills are visible but not loadable

The org skill library serves two consumers with different jobs: the **design
agent** (the agents service — spec/design editing and task planning) and the
**coding agent** (the remote-worker runner, which builds a component). Both were
served one undifferentiated catalog. `scanCatalog` states it outright — the
frontmatter kind is "irrelevant to this scan" — so every skill in the org
snapshot became a catalog row, and `buildSkillCatalog` listed all of them.

The observed cost: designing a Go component, the design agent loads the `go`
skill — 364 lines of pinned builder image, CGO constraints, Dockerfile stages
and health-endpoint conventions it will never act on, because it does not build
anything. The catalog preamble asks it to "call loadSkill ONCE with every
relevant skill's name", and `go`'s description ends "Apply to every Go
component", so it is doing what it was told.

Removing those skills from the catalog does not work. The design agent records
which skills a component's build needs in that component's `design.json`
(`skillsPinned`, formerly `skillsApplied`), and the runner puts exactly those
bodies in the coding agent's context. An agent cannot pin a skill it cannot name, so hiding the row breaks the
handoff that makes the skill reach the build at all.

Ownership kind is not a usable proxy either. Today's twelve skills correlate
perfectly — the four `org`-kind stack skills are coding-agent material, the
eight `platform`-kind ones are design-agent material — but the correlation is
coincidental. `wireframes` is `platform`-kind and serves **both**
(the design agent authors the DSL, the coding agent implements it), and an
org-authored conventions skill is `org`-kind while being squarely design-agent
material. #310 separated ownership from origin and editability for this class of
reason; audience is another axis, not a shade of ownership.

## Decision

**Audience is declared metadata on the skill, and the design agent's catalog
lists coding skills it is not permitted to load.**

- Skills carry `metadata.aep.audience`, a **list** over `design` and `coding`.
  A list rather than a single value because dual-audience is real today
  (`wireframes` is `[design, coding]`), and a singular field invites
  duplicating a skill to serve both.
- **Absent means both.** Narrowing is opt-in: every unmarked skill — including
  every skill an org authors without knowing the field exists — behaves exactly
  as it does now. Nothing an org owns silently becomes unavailable.
- The catalog renders audience **structurally**, grouping rows into what this
  agent may load and what it may only pin. Stating the rule once, from the
  field, keeps it identical for org-authored skills and impossible to contradict
  by prose; descriptions go back to their one job — saying when a skill is
  relevant.
- `SnapshotSkillSource.load()` **refuses** a skill whose audience excludes this
  consumer, with a message naming the alternative ("pin it via `skillsPinned`").
  A refusal indistinguishable from "no such skill" would invite the agent to
  conclude the catalog lied and skip pinning — the worst outcome, since the pin
  is how the guidance reaches the build.
- **No audience is communicated between services.** Each enforcement point
  knows which side it is at compile time: the agents service is always the
  design side, the runner always the coding side. Audience is read from the
  skill and compared against a locally known value — no request field, no
  contract change.

Per-component relevance is deliberately **not** a catalog concern. "This is a
frontend component, it does not need `go`" is expressed by not pinning `go` on
that component. Filtering the catalog per component would also break the
prompt-cache invariant (`buildSkillCatalog` must be identical across a
conversation's turns) and cannot work anyway, since one turn may touch a
frontend and a backend component together.

## Consequences

- **This reverses #310's axis-4 ruling**, which held that audience is "carried
  by the description's triggers + per-audience `references/*.md`, **not** a
  field". Prose cannot refuse a load. Once enforcement at a seam was the goal, a
  machine-readable declaration became unavoidable.
- Migration is a frontmatter stamp on the shipped library: `[coding]` on `go`,
  `react-webapp`, `api-management`, `thunder-authentication`;
  `[design, coding]` on `wireframes`; the rest left absent or marked
  `[design]`.
- **Enforcement is asymmetric.** The design side has a seam we own:
  `load()` refuses and names the alternative. The coding side has no comparable
  seam — the runner allows every skill the mirror contains, because the mirror
  *is* the filtered set and second-guessing it there would put the same policy in
  two places. So what the BFF writes is the whole control, and the direction that
  is enforced is the one that was actually failing. (The SDK does gate skill use
  behind an allowlist, but the runner is not the right place to make an audience
  decision — see `runners/remote-worker/design/decisions/ADR-0003`.)
- The permissive default means an unmarked org-authored skill remains loadable
  by the design agent. That is the status quo, not a regression; stamping it
  later needs a record of which skills a build actually invoked, which the
  platform does not collect yet.
- A dual-audience skill's body is read by both agents, so it must stay useful to
  both. Splitting audience-specific detail into per-audience `references/*.md`
  remains available and is unaffected by this decision.

## Amendment 2026-10-06 — AE Studio

The design side is now `ae-design-agent`, a container of the organization's AE
Studio pod ([ADR-0040](ADR-0040-design-work-runs-in-the-organizations-ae-studio.md)).
Read "the agents service" above as `ae-design-agent`. The audience rule is
unchanged: it still knows it is the design side at compile time.
