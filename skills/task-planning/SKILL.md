---
name: task-planning
description: Use when planning implementation Tasks from a design — the plan turn that covers the milestone's in-scope stories with one Task per feature per design component (plus a foundation Task per component), wires dependsOn, and writes each Task's body.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Task planning

Cover the milestone's in-scope stories. The instruction carries a
**"Milestone scope"** section the platform computed: each in-scope story is
marked COVERED (an existing Task already serves it) or NEEDS TASKS. Your job
ends when every NEEDS TASKS story is served by a Task; COVERED stories'
existing Tasks are reference, never rework. With no scope section, plan every
component that needs work — same rules, whole design in scope.

**A Task is one feature in one design component.** A story is served by the
component that cites it (`stories` in `components/<name>/design.json`, derived
from the cell); its ID names its feature (`F2.3` is F2's). Never invent a
component; a story no component cites is the design's gap — say so in your
final text and recommend extending the design, never a Task without a home.
The platform stamps each Task's "Serves stories" block from the design's
citations and the Task's feature — you never write it.

## What a Task is

The scope section lists the **features this version builds** (each with the
features it needs) and the **product-wide requirements it carries**
(`P1: … (applies to all)`).

- **One Task per feature per component that serves it**, with
  `feature: "F2"`. A component citing stories of F1 and F2 gets two Tasks; a
  component citing none of F3's gets no F3 Task. Title it after the work in
  the reader's words — "Approvals in the API", "Approvals in the web app".
- **One foundation Task per component**, with `feature: "foundation"`, when
  the component has work no single feature owns: its project setup on a first
  build, and the product-wide requirements that reach it (a currency rule,
  an audit trail). Its body names which P items it builds. No shared work →
  no foundation Task.
- **Plan in build order**: every foundation Task first, then the features in
  the order the scope lists them (a feature after the ones it needs), and
  within a feature the providers before their consumers. The platform links
  each Task to its component's foundation and to its component's Tasks for
  the features it needs, so planning them first is what turns those links
  into issue numbers.
- **rationale** is one sentence: why this Task exists.
- **dependsOn** are **component names** from the design's edges: if
  `order-service` calls `user-service`, its Task depends on
  `["user-service"]`. Never issue numbers; never platform infrastructure
  (databases, gateways, IDPs). For every component edge A→B, A's Task lists B
  — the platform links it to B's Task for the same feature, or B's foundation
  when B has no work in that feature. `dependsOn` carries the build order (a
  cycle is rejected; break it).

With no feature list in the scope (an older version), plan one Task per
component and omit `feature`.

## Dependency kinds and gates

| Dependency kind | Ordering effect | What the rationale records |
|---|---|---|
| `component` | consumer's Task lists the provider in `dependsOn` | the build-order edge |
| `org-service` | none — the provider lives in another project | the cross-project binding |
| `external` | none | names the value-collection **gate** |
| `platform-resource` | none | names the provisioning **gate** |

**Gates are flagged, never minted**: the platform authors gate issues; you
emit no Task for a gate. Each design dependency is accounted for exactly once
— in `dependsOn` (component kind) or in a rationale (the other three).

**A dependency is read off the design, never inferred from the PRD.** The
entries this table classifies are the component's `design.json`
`dependencies[]` — the edges `design.cell` draws — and nothing else. A system
the PRD's prose names that no edge reaches is either a constraint the design
resolved another way (`domain-model.md` says how) or a gap in the design: name
it in your final text and recommend extending the design. It goes in no Scope,
rationale or References line — the coding agent has no contract to build a
planned dependency against, and nothing downstream can tell it was invented.

## Fresh and incremental are the same flow

- **Pending Task of an affected component** → `updateTask` it (re-state scope,
  refresh `dependsOn`, rewrite the body) rather than planning a duplicate.
- **Component work already done, new stories arrived** → plan a **delta** Task
  for just the new work, distinctly titled, with the feature the stories
  belong to.
- **In-flight work** → `updateTask` with a note that the change lands on top;
  never silently rewrite its scope.
- **Untouched components and COVERED stories** → do nothing. Silence is
  correct.
- **Obsolete component** (has a Task, gone from the design) → `updateTask`
  with an obsolescence note; a human closes it.

Split a feature's work in one component further only when a single PR
physically cannot land it (e.g. a migration must merge before feature code).

## Write the bodies in the same turn

After planning, write every planned Task's full body via `updateTask` before
the turn ends — `## Scope` (the concrete work, citing the component's
design.json and its openapi.yaml/wireframes.dsl), `## Acceptance` (what done
means, at work altitude — the validation oracle owns product acceptance), and
`## References` (the spec paths the coding agent reads). A Task without a body
is unfinished planning.

For a `web-application` component the wireframe is the screen contract, so
its Task always carries two more things: the path
`specs/design/components/<name>/wireframes.dsl` under `## References`, and a
`Screens:` line under `## Scope` naming every `screen` in that file the Task
covers (`Screens: RiskQueue, MyRisks, NewRisk, …`). Names only — the elements
live in the DSL, and listing them in the issue would go stale the moment the
wireframe is edited. The coding agent's `wireframes` skill turns the names
into pages.

**Map the stories to the flows, and check the mapping yourself.** You hold
both the story list and the wireframe, so you are the one place the two can be
compared — the designer's coverage pass is not one you inherit on trust. Read
the `flow` blocks and write a **`Flows:` checklist** under `## Scope` — one
item per flow, numbered `Flow 1`, `Flow 2`, …, carrying the flow's name, its
`role`, the stories it walks, and its `description` line from the DSL. (`F1`
alone is a feature's ID, so a flow is never numbered that way.)

```markdown
Flows:

- [ ] **Flow 1 · Submit an expense**
  An employee files a claim and tracks its approval.
  Persona: Employee
  Stories: F1.1, F1.2, F1.4
  Walk: MyClaims → NewClaim → ClaimDetail
- [ ] **Flow 2 · Approve a claim**
  A manager works the pending queue and decides a claim.
  Persona: Manager
  Stories: F2.1, F2.2
  Walk: ApprovalQueue → ClaimReview

No flow: F1.3 (sign-in — platform SSO owns the page), F3.1 (nightly export
job, no view).
```

One labelled line each:

- **`Flow 1 ·` + the flow's name**, numbered in the order the DSL declares them —
  the number is how a reviewer names one exact journey.
- **The flow's `description`** from the DSL, straight under the title,
  unlabelled: it is prose, and it says what the journey is before the facts
  about it.
- **`Persona:`** — the flow's `role`. Write `Persona: any` for a role-less
  journey rather than dropping the line.
- **`Stories:`** — the story IDs this journey walks.
- **`Walk:`** — the flow's screens in walkthrough order, entry screen first.
  Names and arrows only, no commentary. A screen in two flows appears in
  both; that is the DSL's shape, not a mistake.

**Leave every box unchecked.** The issue states what must be walked; the
coding agent ticks the same list in its PR body. Re-planning rewrites this
body, so a tick recorded here would be wiped — the PR is where progress lives.

Every in-scope story lands either on a flow item or the `No flow:` line. A
story with no view is expected — sign-in and sign-out on a component with an
auth dependency, a backend rule, a scheduled job, an endpoint another service
calls — so put it there **with the reason**. A story that belongs on a screen
and has no flow is a real gap: say so in the same place
(`F2.4 — no flow walks this`) rather than dropping it, so a human sees it
before the work starts. When every story is walked, say so
(`No flow: none — every story is walked above`) rather than omitting the
line, so a reader can tell the question was asked.

## When a tool rejects you

The result names the fix: UNKNOWN_COMPONENT lists the known components;
UNKNOWN_REF lists the addressable refs; DUPLICATE_TITLE means pick a distinct
title; DEPENDENCY_CYCLE shows the path to break. Correct and re-issue — never
re-emit an op that succeeded.
