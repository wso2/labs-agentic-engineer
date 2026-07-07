# Console — Product Requirements (living document)

> **What this is:** the stable product picture of the console — who it's for,
> how it's organized, and what it does. Purpose, loop, personas, IA, and
> non-goals describe the **target product** (baseline set 2026-07-02, before
> any feature shipped); the **feature inventory** is what's actually shipped,
> and **In flight** lists features currently being built.
>
> **Update rules:** a feature entering build adds one line to *In flight*
> (flow step 6); shipping a feature moves that line into the feature
> inventory and amends any affected sections (flow step 8) — both are
> required steps, not courtesies. Keep entries to one line/paragraph + a
> link; detail lives in the feature's GitHub issue (see
> `design/development-flow.md`, ADR-0001).

## Purpose

The Console is the web frontend of the Agentic Engineer Platform (AEP): an
agentic software development platform built on OpenChoreo. The central
concept is the **project** — a network-bounded set of components. A project's
spec files and implementation live in **GitHub**. When a user gives a
requirement, platform agents derive a **design file** (component
architecture) and **validation files**; **coding agents** then implement the
work, and it deploys to the **dev environment autonomously**.

The Console is the human surface over that loop: give requirements, review
and approve designs, watch coding agents work, and reach the running dev
deployment. It is a single-page React app served alongside the `aep-api`
BFF, which is its only backend.

## The development loop & human gates

1. User gives a requirement (creating or extending a project).
2. Agents derive the design file (component architecture) + validation files.
3. **Design gate (blocking, in the Console):** a developer reviews and
   approves the derived design before any coding agent starts.
4. Coding agents implement; their changes merge and deploy to dev
   **autonomously** — there is no human code-review gate. Humans intervene
   on failure, not by default.

## Personas

- **Developer/User** — gives requirements *and* owns the design gate: reviews and
  approves agent-derived design/validation files, steers coding agents when
  they go sideways.
- **Architect / SRE** — uses the admin area to customize the platform's
  agents. In v1 that means agent **instructions and skills**; model/cost
  policies and pipeline shape are later phases (see Non-goals for what admin
  is *not*).

## Information architecture

Approved at section level; per-section detail is defined feature-by-feature.

- **Home — projects list.** Empty state prompts the user to start an app
  development (give a requirement → project is born).
- **Project view**, sections:
  - **Overview** — component map + status, deployment state, recent activity.
  - **Specs & Design** — the requirement, derived design + validation files;
    the blocking design review lives here.
  - **Build** — coding-agent task board; PRs link out to GitHub.
  - **Deployments** — dev environment state and URLs.
- **Admin** — agent customization (instructions, skills). Architect/SRE only.

## In flight

Features currently being built. One line each; **must be emptied on ship**
(the line moves to the inventory below). If a line sits here for weeks,
that's a stalled feature — investigate, don't ignore.

- Spec view — full-screen spec workspace (grouped requirement/design/validation
  file listing, placeholder textarea content, UI-only build trigger) —
  [#80](https://github.com/wso2/labs-agentic-engineer/issues/80) (BE
  handshake: [#81](https://github.com/wso2/labs-agentic-engineer/issues/81),
  ADR-0007)
- Project overview page — spec/build/deployment status cards (versioned),
  components list, project-view tab shell —
  [#77](https://github.com/wso2/labs-agentic-engineer/issues/77) (BE
  handshake: [#78](https://github.com/wso2/labs-agentic-engineer/issues/78),
  ADR-0006)
- Projects listing page — card-grid landing page with server-side search and
  requirement-first create (`/projects/new`) —
  [#71](https://github.com/wso2/labs-agentic-engineer/issues/71) (BE
  handshake: [#72](https://github.com/wso2/labs-agentic-engineer/issues/72),
  ADR-0005)
- Project delete from listing — card overflow menu + repo-naming confirm
  dialog, `repoUrl` joined into the project list —
  [#107](https://github.com/wso2/labs-agentic-engineer/issues/107) (BE
  handshake: [#108](https://github.com/wso2/labs-agentic-engineer/issues/108))
- Legacy console replacement — apps/console gets production serving at :8091
  (Docker/nginx packaging, compose service, OC workload.yaml) while legacy
  keeps :8090; ports flip when the retirement checklist completes —
  [#98](https://github.com/wso2/labs-agentic-engineer/issues/98) (BE
  handshake: [#99](https://github.com/wso2/labs-agentic-engineer/issues/99))

## Feature inventory

One row per **shipped** feature. Newest first. Links go to the feature's
GitHub issue plus any ADRs it produced.

| Feature | Shipped | Summary | Links |
|---|---|---|---|
| _none yet_ | — | — | — |

## Non-goals

Things the console deliberately does not do. Don't re-litigate these per
session; changing one requires a grilling + a decisions entry.

- **Not an IDE.** No code editing in the console.
- **Not a GitHub replacement.** Code, diffs, PR history live in GitHub; the
  console links out, never re-implements.
- **No human code-review gate.** Coding-agent changes merge and deploy to dev
  autonomously; the design gate is the human checkpoint.
- **No OpenChoreo infrastructure management.** Dataplanes, cells, and platform
  wiring belong to OpenChoreo tooling.
- **Dev-environment visibility only (for now).** No prod ops, alerting, or
  observability surface.
- **Admin v1 excludes** model/cost policies and pipeline-shape configuration —
  later phases, not current scope.

## Cross-cutting requirements

- All API access through the generated `aep-api` client; contract-first
  (see `design/api-guidelines.md`).
- Oxygen UI design system throughout (see `design/design-system.md`).
- UI must be fully developable and demoable against the mock layer, without a
  running backend.
