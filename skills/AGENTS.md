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

## Kinds — `metadata.aep.kind` in frontmatter

An absent kind means `org`, which is a real decision, not a default to lean on:

- **`platform`** — AE-owned, read-only in the console. The design-flow skills
  (`start`, `amend`, `grilling`, `prd-contract`, `design`, `cell-design`,
  `architecture`, `security-design`, `openapi-conventions`, `wireframes`,
  `validation-criteria`, `task-planning`) and the coding run's own workflow skills (`aep`,
  `aep-validation`, `playwright-cli`).
- **`org`** — the org-visible stack skills (`go`, `ballerina`, `react-webapp`,
  `api-management`, `thunder-authentication`). Editable and deletable by an org.

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
- Inside `aep`, the tie-break: a rule naming `git`/`gh`/an issue/a PR belongs to
  `SKILL.md`; one naming a path, a file or an env var belongs to the component
  contract. The contract is stated as information rather than a build procedure,
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
- `organization` is the one skill nothing loads: the design service inlines its
  body into every system prompt and keeps it out of the catalog (ADR-0003). It
  therefore carries no `#` title — the composer supplies the heading, and a
  second one in the file renders it twice.
- A skill is prose for a model, so the usual writing rules apply harder: state
  the rule and the reason it exists, never both halves of a choice.
