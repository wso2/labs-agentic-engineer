# ADR-0002: Skills via progressive disclosure (`loadSkill`), pushed in the turn payload

- **Status:** Accepted · **Date:** 2026-06-27 · **Scope:** `@aep/ae-design-agent` main agent

## Context

We want the main agent to follow reusable, named **guidance** ("skills") — house
style, API-design rules, etc. — while editing a spec bundle. Skills are authored as
`SKILL.md` files (frontmatter `name`/`description` + markdown body) and live in a
repo-root `skills/` directory.

Two pulls shaped the decision:

- The AI SDK now offers `uploadSkill` + `container.skills` — Anthropic's *container
  skills*. But that path only activates the skill when the same call carries the
  `code_execution` tool and runs in Anthropic's hosted sandbox. Our agent has no
  code execution; its files live **in-memory in the service process** and never
  reach a sandbox. Container skills would change nothing about how the file tools
  behave. So `uploadSkill` is the wrong mechanism for *guidance*.
- A skill body is large; most turns need none or one. Inlining every body into every
  prompt is wasteful and dilutes attention.

## Decision

Deliver skills as **guidance text via progressive disclosure**, with the caller
pushing skills in the turn request payload.

- **Payload, not disk.** The turn request carries `skills: { name, description,
  content }[]`. The service **never reads skills from the filesystem** — the caller
  resolves them. In evals the harness reads repo-root `skills/` and sends the whole
  library; in production a BFF pushes them. (`src/` writes no files — existing
  service invariant.)
- **Catalog, then load.** The service appends a **catalog** — `name` + `description`
  only, no bodies — to the **end** of the system prompt. A new **`loadSkill(names)`**
  tool returns the requested skills' `content` as one tool result (batched: every
  skill a turn needs loads in ONE call = one agent step, instead of a step per
  skill); the bodies enter context only when loaded, and persist in message
  history thereafter.
- **Corrective miss.** `loadSkill` with unknown names returns a self-correctable
  error result that still carries every skill that DID resolve plus `missing` and
  `available`, never aborting the turn — the model re-calls for the corrected
  missing names only, matching the `editFile` `NOT_FOUND`/`NOT_UNIQUE` convention
  (ADR-0001).
- **Inert when absent.** No skills in the payload → no catalog, `loadSkill`
  unregistered → byte-identical to today's behavior. `loadSkill` is not a bundle
  mutation, so it projects to **no `Change`**.

Wire types (`Skill`, `LoadSkillInput`) live in `src/contracts/sse-events.ts`; the Zod schema is
drift-guarded against them, as with the file tools.

## Alternatives rejected

- **Anthropic container skills (`uploadSkill` + `container.skills`).** Requires the
  `code_execution` tool + a hosted sandbox that cannot see our in-memory bundle, and
  delivers an *executable* capability rather than guidance over the existing file
  tools. Wrong shape for this need.
- **Inline every skill body into the system prompt.** Simplest, but spends tokens on
  bodies the agent never uses and weakens focus as the library grows. Progressive
  disclosure keeps the always-on cost to one catalog line per skill. A **flow's**
  named lineup is the bounded exception (`FLOW_SUPPORTING_SKILLS` in
  `src/prompts/turn.ts`, plus the flow's own skill): that guidance is known before
  the turn starts, so it rides the per-turn prompt — never the system prompt, and
  never the whole library. Corollary for skill authors: guidance a flow is CERTAIN
  to read belongs in a skill, not a `references/` file, because only skills are
  inlinable — the PRD contract moved out of `start/references/` for exactly that
  reason. References stay for what a flow reads only sometimes.
- **Service reads `skills/` from disk (packed into the image).** Considered first;
  rejected to keep the service free of a skills loader, path config, and a Dockerfile
  dependency, and to preserve the "`src/` touches no fs" invariant. Resolution is the
  caller's job — bytes ride in the payload (the existing push model).

## Consequences

- Always-on prompt cost is one catalog line per skill; bodies cost tokens only when
  loaded. A loaded body persists in history (continuity across later turns in the
  same conversation).
- The catalog sits at the **end** of the system prompt and is identical across a
  conversation's turns (caller sends the same library), so the cacheable prefix is
  preserved.
- Selection is two-tier: the caller decides the *available* set; the agent decides
  what to actually read. Negative/selection behavior ("loaded the right one, ignored
  the rest") is observable in the `loadSkill` call stream and testable in evals.
- A third disclosure level, `loadSkillReference`, reads one named aux file (any path under
  `scripts/`/`references/`/`assets/`/extras that `loadSkill` listed for that skill) on demand — registered
  only when some skill in the payload actually carries references, so a references-free library keeps this
  ADR's exact tool set. It serves UTF-8 text only; a miss (unknown skill or path) returns the addressable
  set so the model self-corrects in one round-trip, matching `loadSkill`'s and `editFile`'s convention.
