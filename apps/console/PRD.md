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
  development (give a requirement → project is born). Org-level sidebar (no
  project in the route): **Projects · Resources · Endpoints · Alerts**;
  Settings stays in the footer.
- **Resources** — org catalog of External resources (`/resources`). Register
  via chat + form; environment values stay on the form. A later project that
  needs a registered name reuses it — Build does not re-collect those secrets.
- **Project view** — inside a project the sidebar nav swaps to its sections
  (ADR-0010; no back-item, home is the header brand / project switcher):
  - **Overview** — the Spec → Build → Deploy track, the components and
    dependencies index, and the architecture diagram.
  - **Spec** — the requirement, derived design + validation criteria.
  - **Builds** — the version ledger: one row per version, with its milestone,
    status, duration and start. A row opens that version's
    build — summary card, task list, External resources, coding-agent log,
    build logs (ADR-0021, ADR-0023).
  - **Deployments** — dev environment state and URLs.
  - **Validations** — the runs checking a build against the spec's validation
    criteria.
  - **Issues** — issues the SRE agent raises against the running project
    (placeholder until its feature lands).
- **Admin** — agent customization (instructions, skills). Architect/SRE only.

## Feature inventory

One entry per **shipped** feature — a feature is shipped when its PR merges,
which is also what closes its issue. Newest first; links go to the feature's
GitHub issue plus any ADRs it produced. Features still being built aren't
here: they're the open `console` + `feature` issues.

- Spec view — the design reads as its parts: the rail's **DESIGN** section
  lists *Architecture · Domain model · Security* as documents, then a
  collapsible **Flows** group — one row per key flow, a ghost row while the
  turn is still planning it — and one group per component, every group header
  carrying a glyph and every component holding *Design · API · Wireframe*.
  Presents the bundle shape the platform now writes
  ([ADR-0020, repo-wide](../../docs/decisions/ADR-0020-design-cell-is-the-design-root.md)):
  the cell is the design root, the domain model and each key flow are one
  diagram per file, and *Design overview* retires with the file it named —
  [#686](https://github.com/wso2/labs-agentic-engineer/issues/686)
  (no contract change)
- Point at a passage, say what should change — **any** markdown spec document,
  not just the PRD's lensed lines. A drag snaps to whole blocks on release
  (a partial paragraph becomes the paragraph, a heading takes its section) and
  offers a single chip: **nothing opens, nothing takes the keyboard**, so
  select-and-retype, copy and delete still mean what they always did. The chip
  (or ⌘K, or a lens) opens one box with two sends — **Change** rewrites the
  selection in place and leaves the chat panel shut, because the document is the
  feedback and a panel would cover the very thing being changed; **Discuss**
  sends the same selection as a grilling and opens the panel to it. What travels
  is an **anchor that locates rather than carries** — the file, and a name per
  selected node (markdown names a block by a bounded excerpt of its rendered
  text) — so the agent resolves it against the CURRENT document rather than a
  photograph of one. It rides as metadata beside the user's words, never folded
  into them, and the transcript shows it as a frozen tag above the message that
  is never re-checked: when the agent cannot find what was named, the agent says
  so in its reply. **The PRD's lens catalogue is re-cut in the same change**
  (#652): an `*assumed*` run offers **Agree · Discuss** — Agree is a direct
  edit that strips the flag, no agent turn, live while an agent holds one —
  and every bullet offers **Discuss**, which opens the same aim box with Enter
  sending Discuss.
  `/settle` on a flagged line is retired; it stays over the Open Questions
  section and on each question —
  [#666](https://github.com/wso2/labs-agentic-engineer/issues/666)
  (ADR-0023, ADR-0024; contract: `TurnInputBody.anchor` / `.intent`, and
  `get-conversation`'s response schema typed at last)
- The project overview is a track of links, not a page of cards — Spec → Build
  → Deploy is one bar with a step numeral per leg and a chevron in each seam,
  and every leg links to the section that runs it. Lit means unsettled and more
  than one leg may be lit (amending a spec while the last version builds lights
  both, with one summary line relating them); a pulse means the platform is
  working and amber-and-still means it is waiting on you. Validation rides the
  deploy leg rather than becoming a fourth gate. Below the track, the components
  and dependencies index sits beside the project's architecture diagram — the
  same `design.cell` render as the spec workspace, sharing its layout, linking
  through to the Architecture view. The activity feed is deleted rather than
  relocated, its whole `features/activity` module with it, and the project's
  status chip moves from three page titles to the toolbar beside the project
  switcher. A project with nothing in it gets the same body, each panel showing
  its own empty state, rather than a substitute page. The overview offers no actions at all: every way of starting work
  stays on the page that owns it —
  [#662](https://github.com/wso2/labs-agentic-engineer/issues/662)
  ([ADR-0022](design/decisions/ADR-0022-the-overview-is-a-track-of-links.md))
- External dependency values are collected on a version's build page, not in
  front of the Build button — provisioning authors every declared key EMPTY at
  build time, so the coding agent gets its env vars defined and Build never
  blocks on a credential nobody reads for another twenty minutes. The values are
  supplied in an **External resources** section on `/builds/$tag`, a peer of the
  Tasks list because outstanding values are work a person must do. The milestone
  run's deploy stage then parks in `waiting` (`external-values`) until every key
  holds a value; the build page's summary card names the blocking dependencies
  and points at the section below it, and the run resumes and deploys on its own
  once the last value is saved. A Registered External is outside the gate — its
  values live on the org record, which no project surface can clear —
  [ADR-0023](../../docs/decisions/ADR-0023-external-dependency-values-are-a-deploy-gate.md)
- Empty states teach *what*, never narrate the *how* — the five flow-narrating
  empty states (Builds, Deployments, Validations, Components, Recent activity —
  the last retired with the feed itself, #662)
  now say what lives on the page and why it is empty, retiring *published* /
  *plan* from all of them; Builds, the one surface a user can act on, gains a
  **Go to the spec** CTA. Wordings live in the lexicon's **Empty states**
  section — [#577](https://github.com/wso2/labs-agentic-engineer/issues/577)
- Builds, rebuilt as a version ledger — **one row per version** (milestone,
  status, duration, start), and the now-first run story it
  replaced moves to its own page at `/builds/$tag`: a summary card, then Tasks,
  External resources, the coding-agent log and the build logs as collapsible
  sections. Provisioning
  gates render as **task rows** rather than a separate stage, each with its own
  way out, which is what retires the stage rail. A task row's five states are
  DERIVED — `derivedStatus` is two-valued, so blocked / in-progress / in-review
  come from `hold`, `blockedBy` and the newest execution — and its second line is
  the issue's newest comment. `/builds/:issueNumber` and `/tasks/:issueNumber`
  swap roles so the version can own the `/builds` segment; old links still
  resolve. **No contract change**: the ledger's remaining cells come from the
  deploy aggregate the layout already polls. It carries no task counts, because
  an untagged list-tasks response cannot be attributed to versions and a
  tag-scoped one would be a GitHub-backed request per row —
  [#609](https://github.com/wso2/labs-agentic-engineer/issues/609) (ADR-0021,
  superseding ADR-0015)
- Resources catalog lives at `/resources` (not Settings). Register an External
  resource through chat that can question then draft the form (secrets stay on
  the form). A new project that needs an already-registered API reuses that
  name; after aep-api restart the catalog still treats it as configured, so
  Build does not ask for the token again —
  [#636](https://github.com/wso2/labs-agentic-engineer/pull/636)
  (ADR-0021; catalog move on
  [#626](https://github.com/wso2/labs-agentic-engineer/pull/626))
- The journey starts itself — creating a project **fires `/start` server-side**,
  so the user lands on the overview with the agent chat already open, the
  transcript showing `/start` beside their own idea (cropped), and the Spec card
  reading **Writing requirements** with **Open spec** as its CTA: generation is
  already underway, so there is nothing left to ask for. A project created WITH
  reference documents declares `referencesPending`, and the platform holds the
  kickoff until the upload lands — they are the primary brief, and an interview
  started before they arrive is conducted blind. The spec view,
  opened before the interview has asked anything, says *"Agent is working on the
  requirements document"*. **Nothing auto-navigates**, and the `?generate=`
  handshake between the overview CTA and the spec view is retired: the CTA that
  still starts an interview seeds the chat from wherever the user is. Its
  remaining forms are resumption affordances — **Try again** over a kickoff that
  died, **Generate spec** on a project nothing ever started —
  [#562](https://github.com/wso2/labs-agentic-engineer/issues/562)
  (contract: `SpecStage.agent`, `CreateProjectRequest.referencesPending`)
- Spec view — the rail is the flow: **Requirements · Design · Validation**
  each carrying state (ready · being worked on · needs attention · not begun),
  documents named as documents rather than files (*Product requirements*,
  *Design overview*, *Validation criteria*), and the app's existing pulse on a
  section an agent is writing. An amber section explains itself in **rows** —
  *N assumptions to challenge*, *N open questions*, *The requirements have
  changed since* — each going where the work already happens. Staleness is
  derived by comparing the requirements against the snapshot the last design run
  read, so nothing is stored and nothing can fall out of sync; **an outdated
  design is refused by the build gate**, joining the refusal Build already shows
  on click. Retires *"Being derived…"*, which claimed work over sections nobody
  had asked for —
  [#575](https://github.com/wso2/labs-agentic-engineer/issues/575)
  (contract: `SpecStage.designOutdated`)
- Spec view — the turn declares its plan: a skill says what it is **about to
  write** (`declare_plan`, fire-and-forget tool-call-as-UI — ADR-0025), and the
  rail renders the checklist — **ghost rows** holding the coming documents'
  places, a pulse on the one being written, and an honest **count** (*2 of 6*)
  that grows in waves because the cell fixes the component set mid-run. Every
  status is derived from the mutation stream, never self-reported. A clean
  turn's plan dissolves; a dead turn leaves its **wreckage** — done ticks, one
  error, the remaining ghosts — surfaced through the attention chip until the
  next declaring turn replaces it. The **editor follows the write** and yields
  to the reader's first manual click (ADR-0026), superseding the cell's
  yank-back. The chat records each declaration as an activity step (*Planned 3
  documents*) —
  [#576](https://github.com/wso2/labs-agentic-engineer/issues/576)
  (contract: `declare_plan` in `@aep/agent-stream`; no aep-api change)
- Overview — the spec card stops rewriting itself: **one button** (*Open spec*)
  in every state instead of three captions walked during a single kickoff with
  no user input, and **one line that always says something** instead of blanking
  the moment the agent asked a question. The card is a destination and never a
  send — every way of STARTING work moved to the spec view, which offers
  **Retry** in exactly two states: under the failure alert when a kickoff died
  (the only state that can be *known* rather than inferred, so the button can
  never appear mid-kickoff), and on an empty workspace with nothing running. The kickoff now fires **inline** with `POST /projects`, so
  the create answers only once the turn exists — which is what makes
  `spec.agent == ""` mean *never started* rather than also *starting right now* —
  [#562](https://github.com/wso2/labs-agentic-engineer/issues/562)
  (no contract change)
- Agent chat — the transcript keeps up with the work: your own message paints
  the moment you send it rather than when the dispatch answers; a turn this
  browser did not send (the creation-time kickoff, or a teammate's) shows who
  started it and what they said, from a display record carried on the turn
  itself — the conversation store only records a turn once it has finished; a
  cold panel looks for a running turn every ~2s instead of every 12s; and a
  question arriving **no longer moves the user** — the pill says the agent is
  waiting and the click is what opens the form —
  [#562](https://github.com/wso2/labs-agentic-engineer/issues/562)
  (contract: `TurnStatus.instruction` / `authorId` / `authorDisplayName`)
- Spec view — the PRD is the interface: each PRD section carries a **code
  lens** firing the command that belongs there — `/actor` on Actors,
  `/feature` on the story list, `/expand` on each story, `/settle` over Open
  Questions — and every flagged line (an `*assumed*` decision, an open
  question) carries its own `/settle`, so the subject comes from what the user
  clicked instead of their memory. Section lenses show at rest, line lenses on
  hover, and all of them go inert while an agent holds the turn. The lenses stay
  the PRD's own, but the affordance no longer is: every markdown document now
  carries selection-anchored aiming beside them, and the flagged line's
  `/settle` became Agree and Discuss (#666). Retires the
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
- Deployments, rebuilt as an environment board — a **card per environment**
  (Development: running version, rollout count, the validation verdict and
  the promotion; Production: the promotion gate and how much live
  configuration is set), then a **ledger with one row per environment that
  runs something** (Version · Milestone · Environment · Status · Validation ·
  Deployed, the Builds ledger's own table), each row opening the
  environment's page at `/deployments/$environment`: a summary card
  (Deployed, Milestone, Validation, the commit that shipped it, a link to the
  build) and the components running there with **Visit** / **Try API** and
  their URLs. Connections keep their Configure surface on a card under the
  ledger. A row is what the environment runs NOW — the platform keeps no
  deployment record, so the design's past deployments, Duration, Redeploy and
  runtime log wait on a backend read. The Development card names how many
  **test users** the project has, one per role, and opens them in a dialog —
  a table of username, masked password with reveal and copy, role, and the
  cold-start account — so the card holds one height whatever the design
  declares. **No contract change** (ADR-0027, amending ADR-0021)
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
  [#151](https://github.com/wso2/labs-agentic-engineer/issues/151)).
  *Superseded twice: the localStorage prompt copy by the project descriptor, and
  the CTA-as-the-way-in by
  [#562](https://github.com/wso2/labs-agentic-engineer/issues/562), which fires
  the kickoff at creation and leaves the CTA as a resumption affordance.*
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
