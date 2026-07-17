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

## Spec versioning

The whole spec — requirements, design, and validation files under the repo's
`specs/` tree — is versioned as **one incrementing `v<N>` git tag sequence**,
cut when the user approves/publishes. There are no per-artifact version
trails (the earlier `v<N>-<M>` design-revision tags are legacy). The console
reads this via `GET /projects/{p}/tags` (`latest` + `specDirty`): the
"vN published" chip is the latest tag, and "draft changes" means `specs/`
moved on GitHub after that tag.

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
- **Project view** — inside a project the sidebar nav swaps to its sections
  (ADR-0010; no back-item, home is the header brand / project switcher):
  - **Overview** — component map + status, deployment state, recent activity.
  - **Specs & Design** — the requirement, derived design + validation files;
    the blocking design review lives here.
  - **Builds** — per-version build history: the selected build's summary +
    its tag-scoped coding-agent task list (Version autocomplete for older
    tags), per-task console log; PRs and issues link out to GitHub.
  - **Deployments** — dev environment state and URLs.
  - **Issues** — issues the SRE agent raises against the running project
    (placeholder until its feature lands).
- **Admin** — agent customization (instructions, skills). Architect/SRE only.

## In flight

Features currently being built. One line each; **must be emptied on ship**
(the line moves to the inventory below). If a line sits here for weeks,
that's a stalled feature — investigate, don't ignore.

- Cost visibility — actual token/USD usage per turn/task/build/project + inline
  historical-average estimates for design/build, with a model cost comparator
  (naive re-pricing + disclaimer); tokens stored, USD derived at read (ADR-0011) —
  [#245](https://github.com/wso2/labs-agentic-engineer/issues/245)
  (BE handshake: [#249](https://github.com/wso2/labs-agentic-engineer/issues/249))
- Deployments page — two-column board (Development / Production): one card
  per component × binding with status chip, release, endpoint URL,
  deployed-at; unbound components show greyed "Not deployed" cards in dev;
  dev column carries the live spec-version chip (status poll's deploy
  aggregate); section header inverts like Builds (#185 convention);
  adaptive 5s/30s poll; reuses components + per-component list-deployments
  (no contract change;
  [#217](https://github.com/wso2/labs-agentic-engineer/issues/217) withdrawn) —
  [#216](https://github.com/wso2/labs-agentic-engineer/issues/216)
- Spec view — formatting toolbar for the markdown editor: StarterKit
  control set (marks, H1–H3 buttons, lists, quote/code, Yjs undo/redo)
  docked as the header of a self-scrolling editor frame, for all md spec
  files — [#206](https://github.com/wso2/labs-agentic-engineer/issues/206)
- Alerts — top-nav notification bell for RCA-agent alert reports
  (org-wide, read-only, client-tracked unread state) —
  [#154](https://github.com/wso2/labs-agentic-engineer/issues/154)
  (BE handshake: [#156](https://github.com/wso2/labs-agentic-engineer/issues/156),
  ADR-0008)
- Alerts — dedicated left-nav section (cursor-paginated list + per-alert
  Stepper progress view: Alert Received / Issue Created / Coding Handover
  / Verify Fix) —
  [#155](https://github.com/wso2/labs-agentic-engineer/issues/155)
  (BE handshake: [#156](https://github.com/wso2/labs-agentic-engineer/issues/156),
  ADR-0008)
- Overview Components table — web apps get their "Open app" URL, console-side:
  each web-application row reads its dev deployment's `endpointUrl` from the
  existing `list-deployments` endpoint (no BE change; `Component.endpointUrl`
  stays as noted contract drift) —
  [#196](https://github.com/wso2/labs-agentic-engineer/issues/196)
  ([#197](https://github.com/wso2/labs-agentic-engineer/issues/197) closed unimplemented)
- Project overview — versioned pipeline (Spec → Build → Deploy) on a single
  adaptive status poll: per-stage version chips (v1, v1+), task/component
  counts from nested ProjectStatus stage aggregates; list-tasks + /tags leave
  the page — [#183](https://github.com/wso2/labs-agentic-engineer/issues/183)
  (BE handshake: [#184](https://github.com/wso2/labs-agentic-engineer/issues/184);
  build-history follow-up: [#185](https://github.com/wso2/labs-agentic-engineer/issues/185))
- Settings → Skills — flat paginated catalogue: alphabetical list with inline
  kind chips (blurb tooltips) replacing the four group sections; numbered
  client-side pagination (10/page) + retained search —
  [#172](https://github.com/wso2/labs-agentic-engineer/issues/172)
  (no contract change)
- Spec view — Build button invokes the build resource: commit-then-build
  (collab flush-on-demand → `POST /build`), lands on the overview; Build
  disabled with a tooltip while an agent turn runs —
  [#162](https://github.com/wso2/labs-agentic-engineer/issues/162)
  (no aep-api change; collab-service flush handler; ADR-0007 amended on ship)
- Project overview — "Generate spec" CTA on the Spec card: persists the create
  prompt to localStorage and, when `hasSpec` is false, seeds a live room turn
  to generate requirements (create does not auto-derive) —
  [#150](https://github.com/wso2/labs-agentic-engineer/issues/150)
  (no contract change; duplicate-generation guard deferred to
  [#151](https://github.com/wso2/labs-agentic-engineer/issues/151))
- Onboarding — first-time credentials wizard for the default org (hard gate on
  incomplete `GET /config`): GitHub PAT + Anthropic key, then auto skills-repo
  bootstrap via extended `/skills/sync` —
  [#102](https://github.com/wso2/labs-agentic-engineer/issues/102)
  (BE handshake [#171](https://github.com/wso2/labs-agentic-engineer/issues/171);
  ADR-0009)
- Spec view — rich design rendering: component-grouped file list, whole-architecture
  cell diagram, per-component wireframes, and Swagger-style API Spec view
  (client-derived, read-only; no committed artifacts) —
  [#149](https://github.com/wso2/labs-agentic-engineer/issues/149) (ADR-0008)
- Settings — org GitHub PAT + Anthropic key credentials, and skills catalogue
  (browse/search/import/sync; no in-console authoring) —
  [#96](https://github.com/wso2/labs-agentic-engineer/issues/96) (BE
  handshake: [#100](https://github.com/wso2/labs-agentic-engineer/issues/100))
- Settings → Skills legacy parity — per-tab routes, categorised catalogue
  (org/platform/custom/imported), MD viewer + monospace editor with preview,
  upload-only import with pull-request guidance —
  [#143](https://github.com/wso2/labs-agentic-engineer/issues/143) (BE
  handshake: [#100](https://github.com/wso2/labs-agentic-engineer/issues/100))
- Spec view — full-screen spec workspace (grouped requirement/design/validation
  file listing, placeholder textarea content, UI-only build trigger) —
  [#80](https://github.com/wso2/labs-agentic-engineer/issues/80) (BE
  handshake: [#81](https://github.com/wso2/labs-agentic-engineer/issues/81),
  ADR-0007)
- Project AI panel — right-hand agent chat on every project route; messages
  run room-scoped collab turns (#86 phase 4): the agent joins the spec room
  as a live peer and edits the shared doc while narrating in the panel —
  [#130](https://github.com/wso2/labs-agentic-engineer/issues/130) (on
  `feature-collab-with-agents`, not aep-rewrite, until the flow completes)
- Console on the proposed contract (#111) — spec view reads via `list-files` +
  lazy `read-file` (supersedes #99); overview Build card from `list-tasks`
  (`/board` is gone); version chips from `/tags` `latest`/`specDirty` —
  [#113](https://github.com/wso2/labs-agentic-engineer/issues/113) (BE
  handshake: [#117](https://github.com/wso2/labs-agentic-engineer/issues/117),
  collab seeding follow-up:
  [#114](https://github.com/wso2/labs-agentic-engineer/issues/114))
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

## Feature inventory

One row per **shipped** feature. Newest first. Links go to the feature's
GitHub issue plus any ADRs it produced.

| Feature | Shipped | Summary | Links |
|---|---|---|---|
| Builds page (Tasks → Builds) | 2026-07-11 | Tasks section becomes the Builds page: current-build view (status, frozen task tally, timestamps) + Version autocomplete over built tags (`?tag=` search param, unknown tag falls back to newest), task list scoped server-side by lineage tag; `/tasks` routes redirect to `/builds`; page title inverts to "Builds" / project-name subtitle on this route only. Read-only v1 — builds start from the Spec view. New `GET /projects/{p}/builds` (one entry per built tag from `workflow_runs` dev rows); `list-tasks?tag=` now filters on the machine-block specTag so pre-#182 label-less tasks still scope. Incident tasks (no lineage) parked → [#190](https://github.com/wso2/labs-agentic-engineer/issues/190). | [#185](https://github.com/wso2/labs-agentic-engineer/issues/185) |
| Legacy console retirement | 2026-07-10 | console-legacy deleted outright (directory, Makefile hooks, docker-compose service). apps/console takes over :8090/`aep-console` in both docker-compose and the OpenChoreo/Thunder cloud path (CORS allow-list, redirect URIs); the one-off `patch-thunder-new-console.sh` transition script is gone. | [#98](https://github.com/wso2/labs-agentic-engineer/issues/98) |
| Tasks page + project-scoped left nav | 2026-07-10 | Sidebar swaps to Overview / Spec / Tasks / Deployments / Issues inside a project (full swap, no back-item; spec workspace auto-collapses it). Tasks: flat list with Pending/Ongoing/Done/Failed chips, 5s polling while active, per-task console-log page streaming `stream-task-log` (SSE) with per-attempt dividers. Issues ships as a placeholder (future SRE-agent issues surface); overview Build card renamed Tasks. No contract changes. Filtering deferred to [#177](https://github.com/wso2/labs-agentic-engineer/issues/177). | [#173](https://github.com/wso2/labs-agentic-engineer/issues/173), ADR-0010 |
| Spec view — Generate / Re-generate design | 2026-07-10 | Phase-aware primary CTA in the Spec view header: **Generate design** when requirements exist but no design, **Build** once a design exists; **Re-generate design** in the Designs section. Fires a design-generation room turn (agent writes `specs/design/…` live). Gated on requirements; Build stays gated on design files. Verified live (7 design files generated + committed). | [#159](https://github.com/wso2/labs-agentic-engineer/issues/159), ADR-0007 (staleness → [#160](https://github.com/wso2/labs-agentic-engineer/issues/160)) |

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
