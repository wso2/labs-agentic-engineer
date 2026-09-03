<!--
Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
Licensed under the Apache License, Version 2.0.
-->

# The narration policy block

The vocabulary a turn's prose should use belongs to the **surface** it is read
on, not to the skill that produced it. `design.cell` is exactly the right word
for someone standing in the repository with a terminal open; on a console screen
it names nothing, and once the artifacts are labelled *Product requirements* and
*Validation criteria* it names things that are demonstrably not there.

The same flow skills — `start`, `design`, `amend` — have to run on both. So
nothing is stripped from the trunk: the difference rides one extra skill, and the
caller says which surface its turn is for.

## The shape

| piece | where |
|---|---|
| the rules | `skills/console/SKILL.md` — a skill, seeded to every org like any other |
| the wire field | `TurnRequest.surface` (`@aep/agent-stream`), validated at the route |
| the composition | `buildNarrationBlock` in `agents/main/prompt.ts` — appends `# Narration policy`; `buildEagerSkillsBlock` re-states its precedence |
| the callers | `aep-api` sends `surface: "console"` at both dispatch sites; the playground sends nothing |

A surface names its own skill (`console` → `skills/console/`), so there is no
second table mapping one to the other, and adding a surface is a `SURFACES` entry
plus a skill directory.

## Why these three choices

**A skill, not prose in this file.** The console is one caller among several and
the rules have to be *absent* for the others. A constant in `prompt.ts` would
reach the playground too, and stripping the paths from the shared skills would
take them from the local run that can actually click them.

**Inlined, not catalogued.** Standing policy, on the `organization` precedent: an
agent that has to remember to load its narration rules will forget one and quote
a path. Being system-prompt-stable is also cache-friendly, and an absent skill
leaves the prompt byte-identical to a surface-free turn — the same graceful
absence the org block has. `UNCATALOGUED_SKILLS` keeps both names out of the
catalog, on every surface: a line offering `console` would advertise a round-trip
returning either text the agent already holds or rules addressed to somebody
else's user.

**Stated twice, because the conflict is in a different message.** The base
instructions let a loaded skill define its own flow's narration, and three skills
had used that freedom to mandate the very output this removes — `design` and
`architecture` both close on a one-line pointer to `specs/design/`. A rule any
skill can opt out of is not a rule, so:

- the meta-rule now yields — *"unless a standing narration policy in this prompt
  overrides it"* — and the block is composed after the catalog and the org
  defaults, the **system** prompt's final word;
- and `buildEagerSkillsBlock` appends the same override to the **user** prompt
  when the turn names a surface. That is where the conflict actually lives: the
  flow bodies are inlined per turn, so they are more recent and more specific
  than anything standing above them. Precedence asserted only in the system
  prompt would be a claim about a message the contradiction is not in.

Those mandates stay in the trunk on purpose — a path pointer is genuinely useful
in a terminal, where the user can open it.

## Why `surface` is sent rather than derived

Everything else about a turn is derived here: the tool set from `TurnSpec.kind`,
the eager skills from the flow, the spec-paths rule from the kind. Two ways to
say something is two ways to disagree.

Surface is the exception because nothing in `TurnSpec` implies it — it is who is
asking, not what is being asked for, so it rides as caller context in
`TurnRequest.surface` rather than being derived alongside the rest. The
playground and the BFF resolve skills from the same library, so "the source
differs per caller" was not available: the same `console` skill sits in both
snapshots, and only the caller knows whether anyone will read its rules. An
unknown value is therefore a pre-stream 400, never a silent fallback — the wrong
answer narrates repo paths at someone who cannot see a file tree.

Ordering, precedence and the four rules are settled under **How the agent talks**
in `apps/console/design/lexicon.md`, which stays the source for the artifact
names the skill pins.
