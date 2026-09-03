# AGENTS.md — skills/

The platform's **one authored skill library**. Every skill any AEP agent loads is
a directory here: `<name>/SKILL.md` plus whatever it ships beside it
(`references/`, `assets/`, `scripts/`).

**`aep-api` (the BFF) is the only consumer that reads this directory**, from the
image path `/app/skills` (`COPY --from=skills`) at runtime. It seeds and
reconciles **every** skill into each org's `org-skills` git repo, which is the
live store everything else reads:

| Reader | How it gets a skill |
|---|---|
| the design-time agents + the console | the org's `org-skills` repo, via the BFF |
| the coding runner | the `.claude/skills/` **mirror** the BFF writes into each project repo — `(audience ∋ coding AND enabled) OR pinned`. The runner reads its own clone and fetches nothing |

So a skill's path to a coding session runs entirely through the org's library,
which means an org's edit to one reaches its builds. The playground has no BFF
and writes the mirror itself (`local_skill_mirror.ts`), applying the same rule.

The library is bind-mounted from the working tree in dev (`setup-k3d.sh` for the
cluster, `pnpm play` for the playground), so **a skill edit needs no rebuild**.

A **tool** a skill names by command is the exception — it is installed, not
mounted, so a change to one is NOT live until it is. `evals/ballerina/AGENTS.md`
has the per-mode table and the loop for measuring whether a change to the
`ballerina` skill did anything.

One result from that loop worth knowing before writing any skill: an instruction
about *how a command is invoked in a shell* does not land — 19/19 lookups stayed
piped across a skill edit saying otherwise, and no thinking block ever mentioned
piping. An instruction about *which command to run* does.

## Kinds — `metadata.aep.kind` in frontmatter

An absent kind means `org`, which is a real decision, not a default to lean on:

- **`platform`** — AE-owned, read-only in the console. The design-flow skills
  (`start`, `amend`, `settle`, `grilling`, `prd-contract`, `design`,
  `cell-design`, `architecture`, `security-design`, `openapi-conventions`,
  `wireframes`, `validation-criteria`, `task-planning`), the `console`
  narration policy, and the coding run's own workflow skills (`aep`,
  `aep-validation`, `mock-verification`) and the browser CLIs they drive
  (`playwright-cli`, `agent-browser`).
- **`org`** — the org-visible stack skills (`go`, `ballerina`, `react-webapp`,
  `astryx-design-system`, `api-management`, `thunder-authentication`). Editable
  and deletable by an org.

Kind decides console visibility and who may edit a skill, and nothing else. It is
`audience` that decides who may *read* one, and the two are independent — a skill
can be the org's to edit while being the coding agent's to read.

## Audience — `metadata.aep.audience` in frontmatter

A list over `design` and `coding`. **Absent means both**, so narrowing is opt-in
and an org-authored skill that has never heard of the field keeps working.

This is the load-bearing field. `[design]` keeps a skill out of every project
mirror, so a coding session cannot see it at all; `[coding]` makes `loadSkill`
refuse it on the design side, and refuse it *distinguishably* from a missing name
(ADR-0014) — the design agent has to be able to name a coding skill to pin it.

A component's `design.json` may pin a skill of any kind via `skillsPinned`, and a
pin overrides both audience and availability: the pinned body is copied and
appended to the coding run's system prompt. Everything else in the mirror is
listed by description and loaded on demand, so a run's startup context does not
grow with the number of components a project designed.

Two names in this library cannot be disabled — `aep` and `aep-validation`
(`spec.RequiredSkills`). They carry the coding run's procedure, the mirror only
copies enabled skills, and the runner refuses to start without them, so a toggle
would take every build in the org down. `PATCH /skills/{name}` returns 409 and
the console renders the switch as unavailable.

**A description is what triggers a load**, since nothing is loaded for the agent.
Write it to name the trigger: `aep`'s says "Load when working a CODING run …
never loaded to author specs/", and `ballerina`'s says "Apply when a component's
`language` is Ballerina. For a Go service, use `go` instead." A description like
"Conventions for writing Go applications" names no trigger and is a defect — that
one was `go`'s, and it was invisible for as long as `go` was preloaded regardless.

## Swapping the UI design system

A web app's UI toolkit is an **organization** decision, and nothing in this
library hardcodes one. Two edits change it, both in places an org may edit:

1. Add the new design-system skill — author `skills/<name>/SKILL.md`, or import
   it through Settings → Skills. Delete `astryx-design-system` if the org does
   not want it available.
2. Point the **UI design system** section of `organization` at its name.

`architecture` reads the name out of that section rather than holding one of its
own. It can, because the `organization` body rides the design agent's system
prompt on **every** turn (`buildOrgDefaultsBlock` in the agents service, not a
per-flow eager skill), so the name is always in context when a component's
`design.json` is written. It then pins that skill on every `web-application`, so
the pin follows the org's choice with **no platform-skill edit** —
`architecture` is `kind: platform` and read-only in the console, which is exactly
why the name cannot live there. An empty section means web-app builds carry only
the stack skills. `astryx-design-system` is the shipped default, nothing more.

A design-system skill must declare four things to work in that slot:

- **`metadata.aep.kind: org` and `metadata.aep.audience: [coding]`.** A design
  system is built against, not designed with; `[coding]` is what puts it in the
  project mirror, and `org` is what lets an org edit or delete it.
- **A `## Verify` section** naming the one command `react-webapp`'s verify
  sequence should run for it, or nothing if it has none.
- **Which of its own defaults the platform overrides.** Every vendor's
  quickstart assumes a project it scaffolded itself; `react-webapp`'s deployment
  facts (no `base`, the platform's own nginx assets, `window._env_`, one
  `tsconfig.json`) win, and
  the skill should say so wherever its own docs would mislead.
- **Its ownership boundary.** The design system owns UI under `src/`; the data
  layer (`openapi-fetch` + the committed `src/generated/` client) stays
  `react-webapp`'s.

Only `organization` and the design-system skill itself may name a design system.
A vendor name anywhere else in this library is a defect — it is the thing that
would make a swap need more than the two edits above. That includes
`references/*.md`: a mirror copies a skill's whole directory, so a vendor name in
a reference reaches a coding session exactly as a body would. Check it before
changing a web-app skill:

```bash
grep -rniE 'astryx|@astryxdesign' skills/ --include='*.md'
```

Only `skills/organization/SKILL.md`, `skills/astryx-design-system/**` and this
file should match. A hit anywhere else — especially in `architecture`, which is
`kind: platform` and read-only in the console — means an org can no longer swap
its design system without a platform change.

## Who owns what

- **`aep` is the umbrella**, and it is split by reader. `SKILL.md` is the **run**
  (start the cycle → work the issues → finish) and only the lead ever reads it.
  The platform contract every component obeys — App Path, port, config + error
  shape, CORS ownership, how a dependency's contract is found, green, and the
  rails that bind anyone touching the filesystem — is
  `references/component-contract.md`. That file is what a **fan-out subagent**
  reads: it gets its contract from its prompt, never by loading this skill, so
  the fan-out section names the file and a rule that is not in it does not reach
  an implementer. The lead reads it too, for inline work and for authoring
  `workload.yaml` (whose format is `references/workload-and-wiring.md`).
- **Stack skills own only their stack**: layout, `Dockerfile`, libraries, the
  verify command, their own pitfalls. Restating a platform-contract rule in a
  stack skill is a defect — it is preloaded context paid twice, and the two
  copies drift (a live example: the deny-list once banned CORS middleware
  outright while `go` required it for an unmanaged service). A cross-stack
  *practice* ("read config in one place at startup") is the contract's; **which
  file it lands in** is the stack skill's.
- **A tool documents itself; a skill points at it.** Where a stack skill drives a
  tool the platform ships, the verbs, flags and output rules belong in that tool's
  `--help`, and the skill says to run it. Same argument as above and a sharper
  version of it: the two ship on different clocks — a tool rides a runner
  image, a skill rides the org's library — so a copy here goes stale with nothing
  to catch it. `ballerina` is the worked example: `bal library --help` carries the
  chain and how to read a document, the skill carries only what surrounds the tool
  (`packages/bal-library-tool/design/decisions/ADR-0011-…`).
- Inside `aep`, the tie-break: a rule naming `git`/`gh`/an issue/a PR belongs to
  `SKILL.md`; one naming a path, a file or an env var belongs to the component
  contract. That is why the **status line** — the one `gh` a fan-out subagent may
  run, keeping its own issue's newest comment current — is handed down through
  the fan-out prompt and not written into the contract the subagent reads
  (`runners/remote-worker/design/decisions/ADR-0010-the-issue-is-the-status-line.md`). The contract is stated as information rather than a build procedure,
  so it reads the same for a component's first line and for a change to one that
  shipped weeks ago.
- Niche material only some runs need goes to `references/`, not into a body.
- **Every reference is mode-neutral.** A mirror copies a skill's whole directory
  and only `SKILL.md` is ever overlaid, so a reference reaches a playground
  session byte-identical — and the `aep` skill lets an agent read its own
  `references/`. Mode-specific text therefore stays in `SKILL.md` even when it is
  long (the branch-identity procedure is there for this reason alone).
  `workflow_skill.test.ts` fails on platform mechanics in a reference, so this is
  caught at CI, not in a local run.
- **A mechanics library is not an entry point.** `grilling` owns the question
  tools and `prd-contract` owns the PRD's shape; neither writes a document, so
  neither is a flow a user can usefully fire — a flow skill (`start`, `amend`,
  `settle`, `design`) owns the artifact and loads them for the mechanics. Nothing enforces
  that: the catalog offers every name, so a skill with no artifact contract says
  so in its own body and names the flow to fire instead.
- **Instructions are as short as they can be and stay unambiguous.** State the
  rule and the failure it prevents; the maintainer's history behind it goes in
  this file or an ADR, not into every run's context.

## `overlays/` — how local mode is made

`skills/aep/overlays/local.md` turns the `aep` skill into the playground's
local-mode workflow (a plain project dir, `issues/*.md`, no remote, no PR). The
authored `SKILL.md` is the **platform** run, with no mode markup in it; the
overlay is a list of anchored edits applied when the playground writes its skill
mirror for `mode: "local"`. Grammar and rationale: the overlay's own header, plus
`runners/remote-worker/design/decisions/ADR-0004-library-owned-workflow-skills.md`
(the overlay mechanism) and `ADR-0005-the-workflow-rides-the-project-mirror.md`
(how a skill reaches a session at all).

Three things to know before touching either file:

1. **`overlays/` is compose-time input, not skill content.** No mirror ever
   copies it into a session, and `aep-api`'s `loadLibrary` never seeds it — so no
   org repo has one either. Both are pinned by tests; don't route new content
   through it.
2. **Every anchor must match exactly once, or the run fails at startup.** That is
   deliberate: a silently-missed anchor would leave the platform's `gh`/PR
   procedure in a local session. If you reword a passage the overlay anchors,
   the playground breaks loudly and you re-anchor it.
3. **The platform text is the trunk.** Overlay a passage only when the unmodified
   version would make a local run attempt something impossible. Prose that
   exists in both files is the cost being controlled — `workflow_skill.test.ts`
   caps it, and the cap only ratchets down. Reading an inert paragraph is far
   cheaper than maintaining a second copy of it.

## Reading the workflow skill by hand

`make workflow-skill` prints `skills/aep/SKILL.md` exactly as a dispatched run
reads it, and `MODE=local make workflow-skill` prints the playground's composed
body. The github one is the authored file verbatim, so it is only worth running
for local mode — whose text is derived and therefore exists in no file. It runs
the same composer a run runs, so there is no second copy to drift.

To use these skills in your own Claude Code, copy the directories into
`~/.claude/skills/` (or point a project's `.claude/skills/` at them). Nothing
assembles a plugin any more — a coding session reads a plain skills directory,
which is exactly what your own Claude Code reads.

## Conventions

- The directory name and the frontmatter `name:` must match — `loadLibrary`
  warns and **skips** a mismatch, so the skill silently disappears from every org.
- Keep a description to one sentence that names when to load the skill, not what
  it contains. It is the only part of a skill most agents ever see.
- Two skills are never loaded, because the design service inlines their bodies
  into the system prompt and keeps them out of the catalog: `organization` (the
  org's standing defaults, on every turn) and `console` (the narration policy,
  on turns whose caller named the console surface). Both therefore carry no `#`
  title — the composer supplies the heading, and a second one in the file
  renders it twice.
- `console` is the surface's vocabulary, not a flow's. It exists because the
  same flow skills run in the console and in a terminal, where a repo path is
  the right word: the trunk stays byte-identical everywhere and the difference
  rides this one skill. Its artifact-name table pins
  `apps/console/design/lexicon.md`, which remains the source — a disagreement is
  settled there, not here.
- A skill is prose for a model, so the usual writing rules apply harder: state
  the rule and the reason it exists, never both halves of a choice.
