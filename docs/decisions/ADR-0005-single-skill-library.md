# ADR-0005: A single skill library, drift-guarded across both agent paths

- **Status:** Accepted
- **Date:** 2026-07-06
- **Context:** AEP drives two agent paths that consume "skills" (authored
  `SKILL.md` guidance, not code): the **BFF architect** (design generation via
  `aep-api` → `services/agents`) and the **playground / design agent** (reads the
  repo directly). These had drifted into two disjoint skill sets:
  - Repo-root `skills/` (high-level-architecture, openapi-conventions,
    excalidraw-wireframes, task-breakdown) — pushed ONLY by the playground.
  - `aep-api/skills/builtin/` (go, react-webapp, api-management,
    thunder-authentication) — `go:embed`-ed, DB-bootstrapped, pushed ONLY to the
    BFF architect; these four did not exist at the repo root at all.

  Only `task-breakdown` had a byte-identity drift guard. Worse, the architect's
  dependency-authoring guidance was ~40 lines of prose INLINE in
  `services/agents/src/agents/architect/prompt.ts`, overlapping and already
  drifting from the `high-level-architecture` skill (a per-dependency-description
  rule diverged). Two sources of truth for the same guidance is a defect factory.

## Decision

**Repo-root `skills/` is the single authored source of every skill.** Both agent
paths consume the same bytes:

| Skill(s) | Repo-root source | How the BFF gets it | Catalogue? |
|---|---|---|---|
| go, react-webapp, api-management, thunder-authentication | `skills/<name>/` | embedded copy in `aep-api/skills/builtin/`, bootstrapped into the `skills` table | yes — design-attachable, per-component |
| high-level-architecture | `skills/high-level-architecture/` | embedded copy in `aep-api/skills/architect/`, pushed directly on every architect call | **no** |
| task-breakdown | `skills/task-breakdown/` | embedded copy in `aep-api/skills/planner/`, pushed directly on every plan/detail call | no |

**Go `embed` cannot cross module boundaries**, so `aep-api` keeps byte-identical
*copies* of the skills it ships. Each copy is a `go:embed`-only mirror; a Go test
(`aep-api/skills/embed_test.go`) reads both the embedded copy and the repo-root
source and fails loud — with a `cp` fix hint naming both paths — the moment any
pair diverges. This is the drift guard, extended from the single task-breakdown
test to a loop over every builtin plus a guard for the architect copy.

**high-level-architecture is pushed directly, NOT bootstrapped into the DB
catalogue.** The architect resolves its catalogue builtins from the `skills`
table (`resolveArchitectSkills` → `skillSvc.List`), so a DB row was one option.
We rejected it: the four stack skills are *design-attachable, per-component
coding* skills (they seed `skillsApplied` and appear in the console catalogue);
high-level-architecture is *design-authoring* guidance about the design and its
dependency edges — it is not attachable to a component and must not seed the
applied set. So it rides its own optional wire field
(`ArchitectRequest.HighLevelArchitectureSkill` → `ArchitectInput.highLevelArchitectureSkill`),
loaded from the embedded default (`architect_skill.go`, mirroring the planner's
`planner_skill.go` / `TaskBreakdownSkill`), and inlined FIRST under "Platform
skills — MUST consult" in `buildUserPrompt`. The architect prompt keeps only
wire/output scaffolding (the four kinds named + the never-emit-`status`/`reason`
invariant) and points at the skill for all authoring judgment.

## Consequences

- One edit to a repo-root `SKILL.md` reaches both agent paths; the design agent
  now also catalogues the four stack skills (name + description only — bodies
  load on demand, so no token-budget blow-up).
- Adding/removing a shipped skill = edit `skills/<name>/`, `cp` into the matching
  `aep-api/skills/{builtin,architect,planner}/` mirror; the embed guard enforces
  the copy.
- The architect prompt and the high-level-architecture skill can no longer drift
  on dependency guidance — there is exactly one source, and `prompt.test.ts` pins
  that the inline judgment is gone and the skill body is transplanted in.
- Cross-module `embed` copies remain a known wart; the byte-identity tests are
  the mitigation, not elimination.
