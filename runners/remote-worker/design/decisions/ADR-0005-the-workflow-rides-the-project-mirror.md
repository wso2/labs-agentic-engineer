# ADR-0005 — The coding workflow rides the project skill mirror

**Status:** Accepted · shipped 2026-08-03 · supersedes the delivery half of
[ADR-0004](ADR-0004-library-owned-workflow-skills.md) (decisions 7–9) and
retires [ADR-0001](ADR-0001-one-mode-composed-skill.md) entirely. ADR-0004's
overlay mechanism stands unchanged.

## Context

Two designs landed in the same week from opposite directions.

ADR-0004 unified the authored trees: `aep`, `aep-validation` and
`playwright-cli` moved into repo-root `skills/`, and the runner assembled a
Claude Code plugin from an explicit **selection** of that library at session
start. Its decision 7 was "the plugin is a selection, not the library" — handing
a coding session the whole library would put `design`'s description in its skill
list, one load away from a mandate to author `specs/`.

Upstream #361 (ADR-0014, ADR-0015, and this package's ADR-0003) rebuilt the
other half: a skill declares `metadata.aep.audience`, the BFF mirrors
`(audience ∋ coding AND enabled) OR pinned` into each **project repo** at
`.claude/skills/`, and the runner reads it from the clone. The per-task
`aep-task-skills` plugin and the runner's skill fetch both went away.

Merged naively the two collide. `desiredMirror` has no kind filter, so the three
runner skills — seeded into every org repo by ADR-0004, and carrying no
`audience`, which means *both* — would be copied into every project repo **on
top of** the base plugin already delivering them. Two `aep` skills in one
session, and in local mode the mirrored one is the un-overlaid GitHub procedure:
the exact failure ADR-0001 decision 7 and ADR-0004 decision 1 exist to prevent.

The requirement that settled it: the runner's skills should reach a session the
**same way every other coding skill does**, and be available in any coding
session.

## Decision

**One delivery path.** A coding session reads exactly one place: the project's
`.claude/skills/` mirror. The three runner skills declare
`metadata.aep.audience: [coding]` and keep `kind: platform`, so `desiredMirror`
copies them like `go` or `react-webapp` — no new kind, and **no change to
`skill_mirror.go`**.

**The plugin mechanism is deleted.** It held nothing but those three skills, a
generated `plugin.json` and a README — no commands, agents or hooks — so with
them mirrored there was nothing left for it to carry. `plugins: []`, no scratch
dir, no ~200 KB copied per run, no `aep:`-qualified names.

Consequences, in the order they were decided:

1. **`skills:` is an allowlist and preloads nothing** (#361's measurement, not
   ours). So the whole mirror goes into it, and guidance that must be in context
   travels on the system prompt instead.
2. **An always-on set, distinct from a pin.** `runner.ts` reads `aep` (every run)
   and `aep-validation` (validation tasks) from the mirror and appends them to
   the `claude_code` preset. A pin is the design's call about a component; this
   is not — no `design.json` decides whether a coding run follows the coding
   workflow. `playwright-cli` is deliberately NOT always-on: it is mechanics a
   validation run may or may not reach for, `aep-validation` names it, and a
   description-triggered load is the right price.
3. **A missing workflow is fatal** (`requireWorkflowBodies`). The mirror's writes
   are best-effort by design — none may fail a creation, publish or dispatch — so
   this is where that becomes visible. Every other skill degrades: a dangling pin
   warns and the build proceeds. The workflow cannot, because a session without
   it does not do a smaller version of the job, it improvises one and reports
   success. **No image fallback**: two sources drift, and the fallback path would
   silently discard an org's edit to the skill on exactly the runs that hit it.
   The check lives in `startCodingRun`, not in each entrypoint, so no new caller
   can start a procedure-less session.
4. **`PATCH /skills/{name}` refuses to disable the always-on set**
   (`spec.RequiredSkills`, 409). Availability is deliberately not gated on
   `editable` — an org admin may withhold a read-only platform skill — but the
   mirror only copies enabled skills, so disabling `aep` would take the procedure
   away from every build in the org and decision 3 would then stop them all.
   Refused rather than force-copied in `desiredMirror`: copying it anyway would
   leave the console showing `aep` as off while every build loaded it, making the
   flag a lie. The console reads `required` off the contract and renders the
   toggle unavailable, so the control explains itself instead of only failing.
5. **The overlay is applied while writing the mirror**, in
   `local_skill_mirror.ts`. Production mirrors the authored trunk, which is what a
   dispatched run should read; the playground has no BFF, writes its own mirror,
   and composes `mode: "local"` into it. So the composer is reached from the only
   point where a local run's skills are written, which is what keeps it
   unskippable. `skill_overlay.ts` is untouched and ADR-0004's safety argument
   holds: every directive must match exactly once or the run dies at startup.
6. **`overlays/` is filtered by every writer, permanently.** `loadLibrary` already
   skips it, so no org repo has one and the BFF's mirror cannot carry one; the
   local writer has to filter explicitly. The `aep` skill permits reading its own
   directory, so an overlay beside `SKILL.md` is a second, contradictory
   procedure the agent can find.
7. **Mode no longer reaches `runner.ts`.** It is the mirror writer's parameter
   now, and github/local is a property of *what was written*, not of the session.
   `BaseAgentConfig` (library path, compose dir, base preload) is gone with it.
8. **`$AEP_SKILLS_DIR` is `<workspace>/.claude/skills`** — same variable, new
   value. ADR-0004 decision 9's argument is unchanged: the runner is the only
   layer that knows where the skills are, so it stamps the path and no skill
   hardcodes one. The component contract a lead hands to fan-out subagents moves
   with it.

## Measured on the playground, docker mode, two components

A `GET /hello` service plus one React screen, run end to end after the change:

- **10 skills mirrored, 8 withheld**, every withheld one design-only
  (`design`, `start`, `grilling`, `high-level-architecture`, `task-planning`,
  `task-breakdown`, `cell-architecture-dsl`, `validation-criteria`). The copy rule
  is doing the work ADR-0004 decision 7 used to do with an explicit selection.
- **No `overlays/` in the mirror**, and the mirrored `aep/SKILL.md` is the
  local-composed body: two local landmarks present, zero occurrences of
  `gh pr create` or `git push -u origin HEAD`.
- **The lead invoked the Skill tool zero times.** Its workflow body and the 4
  pinned bodies were already in context.
- **Every subagent load succeeded — and this is the finding.** Subagents invoked
  `aep`, `go`, `openapi-conventions` and `react-webapp` on demand, four
  invocations, no rejection. The system-prompt append does **not** reach a
  subagent, so a fan-out implementer starts without the workflow and loads it
  itself. Under the pins-only allowlist this run would have had `aep` *rejected*
  for both subagents — which is exactly #361's silent failure, one level down: the
  build would still have passed, because a subagent that cannot load the workflow
  greps for it instead.

So a pin buys preloading **for the lead only**. That bounds what pinning is worth
and it is the argument for listing the whole mirror rather than only the pins.

## Consequences

- A developer who clones a project repo reads the same workflow the build does.
  It is a committed file now, not a runtime artifact of the runner image.
- An org's edit to `aep` reaches its builds, because the mirror is a copy of the
  org's library rather than of the image's. That is new capability, and new
  exposure: `kind: platform` keeps it read-only in the console, and that guard is
  now the only thing standing between an org and its own build procedure.
- `make workflow-skill` replaces `make runner-plugin`. Local mode's text is still
  derived and still has no file on disk, so the question "what is the agent
  steered by?" still needs a command — but it is now off a run's critical path,
  because a local run can no longer write its composed body under the run dir
  (ADR-0003: `fs.cpSync` into that bind mount fails EACCES).
- ADR-0004 decision 7's hazard is closed by a different mechanism than it
  proposed. `design`'s description cannot reach a coding session because
  `audience: [design]` keeps it out of the mirror — the BFF decides, in one
  place, instead of the runner filtering a library it just read.
- The residual risk: **the mirror is a single point of failure for a build's
  procedure.** Decision 3 converts it into a loud one, which is the trade taken
  deliberately. If dispatch-time mirror writes prove flaky in practice, the fix
  is to make that write fatal at dispatch — not to reintroduce a second source.
