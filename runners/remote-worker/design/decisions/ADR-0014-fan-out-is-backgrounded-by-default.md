# ADR-0014 — Fan-out is backgrounded by default, and the skill is what says so

**Status:** Accepted

## Context

For two SDK generations the coding run's fan-out was forced into the foreground.
The `aep` skill said so in prose — *"Do not use `run_in_background`: it does not
add concurrency — it detaches the subagent, so its steps stop reaching the
progress feed"* — with a matching entry in its deny-list, and a `PreToolUse` hook
(`lib/fanout_foreground.ts`) rewrote the flag on every `Agent`/`Task` call so the
prose could not be ignored. Both rested on measurements against SDK **0.3.220**,
where a backgrounded subagent forwarded nothing: `parent_tool_use_id` was null on
all 252 messages of the backgrounded run, and 80 tool calls reached the feed as
narration. ADR-0002 decision 13 has that history.

Neither half of the reasoning survived the SDK bump:

- **Forwarding.** Two recordings against **0.3.247**, kept as fixtures
  (`test/fixtures/probe1-background-fanout.jsonl`,
  `probe2-lead-ends-early.jsonl`), show a backgrounded subagent's tool calls
  arriving on the parent stream with `parent_tool_use_id` set, a declared
  `task_started` lifecycle carrying `spawn_depth` and the parent, and a
  `task_notification` carrying the agent's report and its totals — including for
  a depth-2 child, attributed to itself rather than flattened onto its parent.
- **The session ending under its children.** Real, and fixed where it belonged:
  the loop that treated the first `result` as the run ending (ADR-0002 decision
  18). The hook was never the fix for it.

What the forcing cost was measurable and large. A foreground fan-out call blocks
the lead for the whole wave: in one real 55-minute run the lead sat inside a
single fan-out call for **41 of those 55 minutes**, unable to resolve the next
component's wiring or review a builder that had already come back.

The hook is deleted, with its module and its test (ADR-0002's v2 amendment,
entry 4), and `TaskOutput`/`TaskStop`/`TaskCreate`/`TaskUpdate`/`TaskGet`/
`TaskList` are off `DISALLOWED_TOOLS`. That leaves the `aep` skill's prose as the
only thing shaping fan-out — and it forbade exactly what the platform now wants.

A second question rides on the first. The library is **one authored file per
skill, shared by every org**, and the guidance that has to change is expressed in
tool names: `Agent`, `run_in_background`, `TaskOutput`. Writing those into the
body makes the library a Claude Code document, and a second runtime would then
read a procedure naming tools it does not have — silently, because prose cannot
fail at startup the way a missing anchor does.

## Decisions

1. **Fanning out in the background is the default shape, and the skill says it
   in one place.** `### Fan-out to subagents` now reads *"Dispatch every builder
   of a wave in the background, in ONE turn"*, and states the reason the lead
   acts on: backgrounding is what lets it keep working while the wave builds. The
   two tests for whether to fan out at all — disjoint App Paths, big enough to be
   worth a subagent — are unchanged, as is everything the section says about a
   subagent's prompt, the walk, the sole git writer and no worktrees.

2. **The lead waits for every subagent before it stages or commits.** This is
   what the hook used to guarantee structurally, and it is the one property
   backgrounding actually endangers: a subagent that has not reported is still
   writing files, so a commit taken early ships half an issue. It is stated as
   its own rule rather than folded into the dispatch sentence, because it fires
   at a different moment.

3. **Inside a subagent, every command runs in the foreground**, and every
   subagent prompt says so. Probe 2 is the measurement: a subagent backgrounded
   its own `sleep`, reported "completed" with the command still running, and the
   shell task was reported `stopped` at session end. A builder that backgrounds
   `npm run build` and reports clean is the same shape, and the run has no way to
   tell that report from a true one.

4. **The lead names the model on the fan-out call.** A walk or a small fix runs
   well on the fast model; a build does not. Nothing else in the run can make
   that choice — the runner pins one model for the session, and only the lead
   knows what a given subagent is for.

5. **The lead's plan lives in the runtime's task list.** One entry per issue,
   `in_progress` when work starts and `completed` when it is committed. The list
   reaches the feed as `work_item {source: "plan"}` rows, so it is the only
   statement of intent a run produces that a person watching can read — which is
   also why those tools came off the deny list (ADR-0002 v2 amendment, entry 8).

6. **The skill names ROLES; the runner binds them.** The body says "the fan-out
   tool", "the wait tool", "the task list", and `lib/tool_glossary.ts` appends a
   per-runtime glossary that resolves each one for the session. The glossary is
   appended **last** — after the workflow body and after any pinned skill bodies
   — because the skill points at it by position ("the tool glossary at the end of
   your instructions"). `systemPromptAppend` in `lib/runner.ts` is that order,
   exported so it is a test rather than a comment.

   A second runtime is one more entry in `GLOSSARIES` and nothing else. This is
   deliberately *not* the runtime port: spawning, translating and settling a
   session is a larger seam, and `progress/claude_adapter.ts` is its other half.

## Consequences

- **Prose is the only guarantee now, and that is the trade.** A lead that ignores
  decision 2 can commit a half-written tree, where the hook made it impossible.
  The pins are in `workflow_skill.test.ts` — the three rules must survive in both
  composed modes — so what CI protects is that the *instruction* exists, not that
  the run obeys it. If a real run is measured breaking decision 2, the answer is
  a hook on the git call, not a return to foreground fan-out.
- **`make workflow-skill` now prints the glossary after the composed body**, in
  both modes, because the skill's roles do not resolve without it and the CLI's
  whole job is to show what an agent actually receives.
- **A tool name in the skill body is now a test failure.** `workflow_skill.test.ts`
  asserts the composed body carries none of `run_in_background`, `TaskOutput`,
  `TaskStop`, `TaskCreate`, `TaskUpdate` or `` `Agent` `` in either mode, and that
  every role it does name is bound by the glossary.
- **The local overlay needed no edit.** Fan-out shape is mode-neutral — the
  playground drives the same runner entrypoint (`src/local.ts` →
  `startCodingRun`), so it gets the same glossary — and the overlay owning a copy
  of any of it would be the duplication `skills/AGENTS.md` caps.
- **Unmeasured on a real build.** The probes are recordings of the SDK's
  behaviour, not of a milestone run under the new prose. The first real run is
  where the idle-time claim gets its number.
