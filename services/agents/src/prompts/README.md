# src/prompts — where the wording lives

Every word this service sends to a model is written here or in
`../agents/main/prompt.ts`. Nowhere else in the repo holds prompt text.

| File | Holds |
|---|---|
| `turn.ts` | The per-turn instruction: what a `TurnSpec` becomes. |
| `../agents/main/prompt.ts` | The standing system prompts — the editing agent's charter, the task planner's, the skill catalog, the eager-skill block, the org-defaults block. |

## Editing a prompt

Edit the string. There is no generator, no JSON source, and no second copy to
regenerate — `make gen` has nothing to do with prompts. Run
`pnpm --filter @aep/agents test`; `test/turn-compose.test.ts` and
`test/prompt.test.ts` pin the structure, not the wording, so ordinary rewording
does not break them.

## Why callers don't compose

Callers send a `TurnSpec` — facts about what the turn is for — and this service
turns it into text:

```
aep-api / playground  ──{kind:"start", idea:"…"}──▶  turn.ts  ──▶  the model
```

The BFF has the database, git and the project descriptor; this service has the
model. Prompt text is an input to a model, so it lives beside the model. Concretely:
a caller cannot express "say it differently", only "this is a start turn with
this idea".

That also means the eager-skill map and the tool set are derived here from
`turn.kind` rather than sent — a console CTA, a typed `/start` and a playground
run all reach the same map, so they cannot drift apart.

This replaced a `strings.json` → codegen pipeline that authored each sentence
once and compiled it into both Go and TS. See `../../design/ADR-0003-turn-composition-lives-here.md`
for what went wrong with that and why this shape is expected to hold.

## What is NOT here

- **The `/<command>` grammar** — `@aep/contracts/commands`. Parsing a command is
  fact extraction; it yields a token, not a sentence. Which skill that token
  loads, and which branch of it, IS wording (`COMMAND_FLOWS` in `turn.ts`): a
  command names the user's intent, a skill names an engineer-facing playbook,
  and deciding that `/feature` means "the amend skill, add-a-feature branch" is
  a sentence about to be written.
- **Skill bodies** — org-authored, read from the turn's `_skills` snapshot
  (`conversation/load-workspace.ts`). A skill is guidance; this directory is the
  frame around it.
