# Console — Product Requirements (living document)

> **What this is:** the stable product picture of the console — who it's for,
> how it's organized, and what it does. Purpose, loop, personas, IA, and
> non-goals describe the **target product** (baseline set 2026-07-02, before
> any feature shipped); the **feature inventory** is what's actually shipped.
>
> **Update rules:** the feature's own PR adds its inventory entry and amends
> any section the feature changes — part of the PR, not a follow-up, since
> merging the PR is what ships it (flow step 6). Keep entries to one
> line/paragraph + a link; detail lives in the feature's GitHub issue (see
> `design/development-flow.md`, ADR-0001). Work **in progress** isn't
> tracked here — the open issues are: `gh issue list --repo
> wso2/labs-agentic-engineer --label console --label feature`.

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
  - **Spec** — the requirement, derived design + acceptance criteria.
  - **Builds** — per-version build history: the selected build's summary +
    its tag-scoped coding-agent task list (Version autocomplete for older
    tags), per-task console log; PRs and issues link out to GitHub.
  - **Deployments** — dev environment state and URLs.
  - **Validations** — the runs checking a build against the spec's acceptance
    criteria.
  - **Issues** — issues the SRE agent raises against the running project
    (placeholder until its feature lands).
- **Admin** — agent customization (instructions, skills). Architect/SRE only.

## Feature inventory

One entry per **shipped** feature — a feature is shipped when its PR merges,
which is also what closes its issue. Newest first; links go to the feature's
GitHub issue plus any ADRs it produced. Features still being built aren't
here: they're the open `console` + `feature` issues.

- Spec view — the PRD is the interface: each PRD section carries a **code
  lens** firing the command that belongs there — `/actor` on Actors,
  `/feature` on the story list, `/expand` on each story, `/settle` over Open
  Questions — and every flagged line (an `*assumed*` decision, an open
  question) carries its own `/settle`, so the subject comes from what the user
  clicked instead of their memory. Section lenses show at rest, line lenses on
  hover, and all of them go inert while an agent holds the turn. Retires the
  composer's `Actions ▾` menu of raw slash commands. **Open questions no longer
  block Generate design** on either side — the console disable and the two
  skill clauses both go — since a recorded gap is information, not corruption
  (the reasoning that already settled dependencies in
  [#526](https://github.com/wso2/labs-agentic-engineer/issues/526)); `deferred`
  survives as the user's *"stop asking"*. A command names the user's intent and
  resolves to a skill server-side, so `amend` stops being what a user reads and
  `/settle` arrives as its own skill, because revision propagates. No contract
  change —
  [#579](https://github.com/wso2/labs-agentic-engineer/issues/579)
- Create flow — says what's about to happen: the subtitle answers only *how
  much detail?* (*"Describe it in your own words — rough is fine."*), the
  repository field states that Agentic Engineer **creates** it in the user's
  organization, a taken repository name resolves to a field-level error naming
  the org rather than a raw alert, the wait reads **Creating your project…**,
  and the examples carry the enterprise persona (expense approval, employee
  onboarding, a support triage agent). Retires **AEP** from user-facing copy in
  favour of **Agentic Engineer**. First feature to draw on the console lexicon
  (ADR-0019) —
  [#561](https://github.com/wso2/labs-agentic-engineer/issues/561)
- Deployments page — one-story rail + environment panel: Development /
  Validation / Production as one numbered rail (Builds-spine vocabulary,
  ADR-0014) with a side panel (version, rollout, endpoints, production
  readiness); per-component env-var editor over `GET/PUT …/configs`;
  validation verdict with criteria counts; promotion via a live-configuration
  dialog gated on per-connection values (values page-state only until a
  promote BE surface exists; no contract change) —
  [#395](https://github.com/wso2/labs-agentic-engineer/issues/395)
- Spec view — wireframe prototype mode: a **Canvas | Prototype** toggle on
  settled wireframes swaps to a single-screen, click-through view — controls
  annotated `-> Target` in the `.dsl` jump screens Figma-prototype style, with
  back/picker toolbar and dead-area hotspot flash; **Open full screen** lands
  on a deep-linkable `/projects/:project/prototype/:component?screen=<Name>`
  route. `@aep/excalidraw-dsl`'s `tryDslToPrototype` compiles per-screen
  scenes client-side (no BE handshake, no contract change; ADR-0008) —
  [#348](https://github.com/wso2/labs-agentic-engineer/issues/348)
- Spec view — readable wireframe canvas: screens compile into a single column
  instead of a two-across grid, and the canvas opens focused on the first
  screen at a legible size with the top of the second peeking below; while an
  agent edits, the viewport pans to the screen being changed instead of
  refitting the whole board, and stays put when nothing identifiable changed.
  `@aep/excalidraw-dsl` stamps each element with its screen so the viewer can
  group per screen; no contract change —
  [#552](https://github.com/wso2/labs-agentic-engineer/issues/552)
- Project create — reference document upload on the "What do you want to
  build?" view. Two groups, both readable by the models: `.pdf`/`.png`/`.jpg`/
  `.jpeg`/`.gif`/`.webp` read natively as file parts, and `.md`/`.txt`/`.csv`/
  `.tsv`/`.json`/`.yaml`/`.yml`/`.xml`/`.html`/`.rst` read as text (≤10 files,
  ≤5 MB each) — attached in a chat-style composer and uploaded post-create over
  multipart to `POST /projects/{name}/references`. References are **transient
  turn inputs, never committed** (ADR-0017): bytes live on the shared
  `/workspaces` volume for the project's life and are overlaid into each turn's
  snapshot at `specs/requirements/references/`, surfaced to the `/start` kickoff
  through the idea-steer channel. No console surface after create —
  [#383](https://github.com/wso2/labs-agentic-engineer/issues/383)
  (BE handshake: [#384](https://github.com/wso2/labs-agentic-engineer/issues/384))
- Agent chat — attach files to a message: the composer takes a paperclip and a
  drop target, the same cards and accepted set as the create view, and chips on
  the sent message that survive a reload. Attachments are **conversation-scoped
  model content** (ADR-0019): attachment BYTES are never written to disk and
  never committed, and the file names are retained as message metadata so the
  chips survive a reload — the bytes ride one multipart `POST
  /projects/{p}/agents/{conversationId}/messages` into the turn and are durable
  only as parts of the conversation's history, which is what makes re-sending
  one free (the agents service dedupes by file name). The agent reads them
  natively — a PDF as a document, an image as an image, every text format as
  text — and the turn prompt NAMES them, so "add this as a separate form"
  resolves to the file the user just attached rather than drawing a clarifying
  question. Caps all restate the
  model's own 20 MiB encoded per-turn budget: ≤10 files, ≤5 MB each, ≤15 MB raw
  in total. Any turn started from the composer carries them — chat, flow and
  `/start` alike — and the create view stays the only door to the project
  reference store —
  [#428](https://github.com/wso2/labs-agentic-engineer/issues/428)
- Spec view — prototype user flows: `wireframes.dsl` declares named
  `flow "<name>"` blocks (optional `role`/`description` lines) listing each
  persona's screens in walkthrough order; the prototype toolbar leads with a
  **User flow** picker that scopes the screen picker to the chosen flow, the
  canvas marks each screen's membership (`Approval queue · Screen 2`,
  `Common · Screen 1`), and `?flow=<Name>` joins `?screen=` on the full-screen
  route. Same client-side derivation, no contract change —
  [#491](https://github.com/wso2/labs-agentic-engineer/issues/491)
- Agent chat — structured question cards: `ask_question` (single) +
  `ask_questions` (batch form) tool-calls rendered as native Oxygen UI cards
  in the activity stream (answer returns as the next turn's plain text);
  grilling interview auto-started on the spec-generation CTA; tool-call-as-UI
  convention in ADR-0012. FE mock-verified; agents-service + platform grilling
  skill via [#271](https://github.com/wso2/labs-agentic-engineer/issues/271) —
  [#270](https://github.com/wso2/labs-agentic-engineer/issues/270)
- Usage & cost — org-wide agent spend on a dedicated **Settings → Usage**
  page: one card per project (incl. deleted projects) with a folded USD cost
  as a plain value, hover for the input/output/cache token breakdown; USD-only
  primary, actuals only. Supersedes the scattered per-phase chips of #245
  (Builds task/build chips + Spec drafting-cycle chip, all removed). Costs
  are **stamped at capture time** from **DB-backed model rates**, so a rate
  change never rewrites history (ADR-0011 reversed) —
  [#291](https://github.com/wso2/labs-agentic-engineer/issues/291)
  (supersedes [#245](https://github.com/wso2/labs-agentic-engineer/issues/245);
  BE handshake: [#299](https://github.com/wso2/labs-agentic-engineer/issues/299))
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
- Settings → Anthropic, coding-agent key — bill the coding agent to a separate
  credential (another API key, or a `claude setup-token` token that bills a
  Claude subscription) while everything else keeps the org's key. "Reuse the key
  above" is the default and is the ABSENCE of a second key, not a stored mode
  (ADR-0016)
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

### Earlier features, with ship dates

Recorded in table form before the inventory settled on the entry format
above. Same meaning: one row per shipped feature.

| Feature | Shipped | Summary | Links |
|---|---|---|---|
| Build-session spine (run rail) | 2026-07-28 | A run renders as **one rail of staged sections**, and every applicable stage exists from the moment the run does — an unreached stage says what it waits for instead of being absent. **Provisioning** comes first (only when the milestone holds gates), as a run-level stage, because `OpenProvision == 0` is genuinely the dispatch predicate: each connection is listed with who is acting on it, which retires the gate branches of the hold notice. Then one **build session** (the console's name for a cycle) per dispatch, each a five-stage spine with a named actor — coding agent (runner) · pull request (agent) · merge (platform) · builds (platform) · deployment (cluster). The agent log lives inside the coding stage; issues appear on the provisioning and coding stages only, attributed from the merge policy's recorded matched set. **Deployment** replaces the cycle verdict: a statement plus a router link to the deployments board, with no cluster read — a `Deployment` carries no commit, so no rollout can be attributed to a session. A collapsed session shows a stage strip, so "which session broke" is answerable without opening anything; builds are read for the newest session always and older ones on expand, keeping the cluster-derived query bounded. `RunCycleView` gains `resolves[]` / `prDraft` / `mergeVerdict` / `mergeReason`, each a fact the event plane already computed and threw away — which is what finally gives the four silent stalls (draft PR, declined merge, refused merge, twice-red build) words on screen. | ADR-0014 |
| Version run surface (Builds page rebuilt) | 2026-07-27 | The backend now executes a version as ONE supervised run over one GitHub milestone, so the Builds page becomes **one version's story, latest by default** — landing straight on the live run, never on a list. Choosing an older version stays a control on that page (the Version picker, `?tag=v<N>`); the overview's stage cards remain a read-only summary. Per run: state chip, origin, terminal reason as a sentence, budget counters, the cycle timeline (branch / PR / merge SHA, all learned from webhooks), prominent **Cancel** on a parked run, and the per-cycle agent feed as an accordion over one SSE stream with `subagent` emitter chips. Issue rows carry durable facts only and no longer navigate; an open dispatch gate renders as a **hold banner** deep-linking the connection drawer; bare human issues get a **Ledger** section. Validation re-keys to the run's verdict on the deployment surface. The run state is the page's only liveness driver — the ten-value derived-status algebra and its poll-stop are gone, and `ProjectStatus.build.tasks` / `deploy.validationIssue` / `deploy.validationUrl` leave the contract. | [#286](https://github.com/wso2/labs-agentic-engineer/issues/286), ADR-0013 |
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
