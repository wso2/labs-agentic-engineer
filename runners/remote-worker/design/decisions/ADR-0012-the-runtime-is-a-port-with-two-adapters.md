# ADR-0012 — The runtime is a port, with two adapters

**Status:** accepted · 2026-09-07 · Amended 2026-09-22 (the loop reads a
classifier, last section); the second adapter is ADR-0015
**Supersedes nothing.** Extends ADR-0002 (run observability) and ADR-0014 (the
tool glossary), which each carved out one half of this seam before it existed.

## Context

An organization's coding runs should be able to run on a runtime other than
Claude Code — the motivation is other model providers, not dissatisfaction with
this one. Before this change, "which runtime" was not a thing the code had a name
for. It was scattered:

- `lib/runner.ts` called `query()` directly, with `model: "claude-sonnet-5"` as a
  literal, a sixteen-name `DISALLOWED_TOOLS` list, three `PreToolUse` hooks wired
  to Claude Code's hook grammar, `settingSources`, `strictMcpConfig`,
  `permissionMode`, and a loopback MCP proxy that exists solely because this
  SDK's HTTP MCP config only accepts a static `Authorization` header.
- `runtime/claude/translate.ts` translated its messages.
- `lib/tool_glossary.ts` bound the workflow's role names to its tool names.

Only the last two were labelled as runtime-specific. `runner.ts` was where a
second runtime would have had to fork.

## Decision

**`runtime/port.ts` is the whole runtime-specific surface, and `lib/runner.ts`
depends on nothing else about a runtime.**

A `Runtime` answers three things: its name, its tool glossary, and
`start(prompt, policy)`. `RuntimePolicy` is the platform's rules for a coding
run, written in vocabulary no runtime owns — where authored files may land, what
a WebSearch query may contain, which skills are reachable, which capabilities are
denied, which model to bill, whether developer diagnostics are on. An adapter
enforces every clause through whatever mechanism its runtime has.

The test for whether something belongs in the port is whether it would be written
the same way for a second runtime. If it names a tool, a hook or an SDK option,
it does not.

**There are two adapters: `runtime/claude/` and `runtime/opencode/`
(ADR-0015).** `runtime/registry.ts` builds the one `AEP_AGENT_RUNTIME` names.

## Where the design's sketch and the repository disagreed

The design (rev 6, §11) sketched the interface. The repo won six arguments with
it, and each is recorded at its field in `port.ts`.

1. **`allowWriteUnder: string[]` → `WritePolicy.allowOutsideProject(target)`.**
   A prefix list is a decision this repo already made and reversed:
   `workspace_guard.ts` allows the temp directory plus **any dot-directory
   directly under `$HOME`**, as one rule, because every toolchain hides its cache
   in one and an enumerated list "silently contradicts the next stack skill
   added" — its earlier version named three and did exactly that.

2. **`deniedTools: string[]` "runtime-neutral names" → `DeniedCapability`
   classes.** There are no runtime-neutral tool names to write; the current list
   is sixteen Claude Code identifiers a second runtime would share none of. What
   the two lists WOULD share is the reason each entry is on it, which
   `runners/AGENTS.md` already states as a sentence. So the port carries the
   reasons and each adapter carries its own names — the same shape the tool
   glossary already uses in the other direction.

3. **`toolGlossary(): {fanOut, wait, stop, edit, write, shell}` → `string`.**
   `lib/tool_glossary.ts` already worked, and its content is more than a name per
   role: it carries the argument that makes each role work
   (`run_in_background: true`, `block: true`). A record of
   bare names drops exactly the part that stopped leads guessing, and the caller
   would render it back into prose anyway.

4. **`skills.preloadBodies` is the WHOLE appendix, glossary included.** The `aep`
   skill points at "the tool glossary at the end of your instructions", so the
   glossary's position is part of the contract and nothing may follow it. The
   runner assembles the appendix from the glossary the runtime supplies
   (`systemPromptAppend`), and the adapter appends nothing of its own.

5. **`mcp.token()` gained `invalidate?()`, `onToken?()` and `onFatal?()`, and
   `tools`.** A token function alone cannot express "this run cannot remint" —
   which is every dispatch with no publisher credentials mounted — and the
   platform, not the runtime, owns what happens when a bearer is reminted
   (persist it to the bearer file, enrol it with the scrubber) or can no longer
   be (end the run). Tool names travel BARE; `mcp__<server>__<tool>` is one
   runtime's convention.

6. **`RuntimeSession.events(): AsyncIterable<RunEventInput>` → `stream` +
   `translate` + `classify`.** This is the largest deviation and the only one
   that gives something up. The repo cannot express a run as a flat event stream without
   changing what the watchdog is told, and that contract is measured:

   - `watchdog.observe([])` RESETS the idle clock, and a heartbeat dropped by the
     rate limiter produces no event at all. "Route on the events that came back"
     silently converts every rate-limited wait into activity — the exact stall
     the heartbeat exists to report. `run_loop.ts` therefore routes by MESSAGE,
     not by event, and says so.
   - `observeRetry` and `observeStream` are two different non-activity signals,
     and the watchdog needs both to name a stall's cause. A flat event stream
     collapses them into "a notice arrived".
   - every raw message is written to `runtime.log`, which a flat event stream no
     longer carries.

   So the session exposes the run at the level `consumeRun` reads it. Collapsing
   those into a flat event stream is a real improvement and a real risk; it is a
   change to the WATCHDOG's contract, not to this port, and it was not made here.
   How the loop tells one message from another is the amendment below.

Also dropped: `RuntimeArtifact.kind: "task_output"` — nothing produces one (a
task's output reaches the feed as `agent_settled.report`), and a kind with no
producer is a claim about a shape nobody has seen. And `RuntimeSession.stop`,
which would have duplicated `stream.stopTask`; the `reason` the sketch gave it
has nowhere to go in the SDK and already reaches the feed as the deadline guard's
own notice.

## What a second adapter had to answer first

Three questions, each of which fails silently when guessed: can every
`DeniedCapability` and guard be enforced PRE-DISPATCH; does the stream declare
an agent's id, depth and parent; is usage reported cumulatively per model, since
`modelcost.SumCost` blanks a cycle's cost on one missing slice. ADR-0015 records
OpenCode's answers.

## Consequences

- `lib/runner.ts` reads as wiring. It no longer imports the Agent SDK.
- `runClaudeQuery` is `startCodingRun`: the function no longer builds a Claude
  query, and a name that says it would mislead the next reader.
- `model` comes from `AEP_AGENT_MODEL`, defaulting to the runtime's own
  `defaultModel`. An org that never opens the setting gets exactly the run it had.
- The glossary table is keyed on the port's `RuntimeName`, which is also the
  contract's `AgentRuntime` value and the `AEP_AGENT_RUNTIME` env value. One
  spelling; `claude_code` became `claude-code`.
- `createWebSearchDlpHook` / `createWebFetchGuardHook` / `createWorkspaceWriteGuard`
  now take the DECISION rather than the inputs to it, so each rule is stated once
  by the runner and enforced by whichever adapter is running.
- `RuntimeSession.usage()` is the adapter's cumulative usage, which the loop
  settles with when a run ends early (deadline, fatal) and no turn carries it.
- The port carries no tool-call watcher. The validation run's progress
  tracker and status line read tool calls through one while they existed; they
  were retired with the move to acceptance scenarios, and the seam went with
  them rather than stay as an unwired runtime-neutral call type in both
  adapters.

## Amendment (2026-09-22): the loop reads a classifier, not a message shape

`RuntimeSession.classify(message) → MessageClass` answers every question
`consumeRun` asks of a message, and the loop branches on the class and on
nothing else — no `type`/`subtype` test outside a runtime's own directory. The
classes are closed and are the loop's own vocabulary: `retry`, `stall_signal`,
`model_wait {streaming}`, `tool_progress`, `turn_end`,
`task_bookkeeping {started?, ended?}`, `init {resolvedSkills}`, `noise`,
`activity` (their treatment is documented at `MessageClass` in `port.ts`). The
watchdog is told what decision 6 requires; only who decides moved.

- **One class per message.** `turn_end`, `task_bookkeeping` and `init` are each
  handled as `activity` plus the one step that is theirs.
- **`noise` is recorded and nothing else** — not translated, not activity, not
  proof of life. A message about the server rather than the run would otherwise
  reset the idle clock.
- **The classifier is per session and may keep state** (Claude Code's rate-limit
  dedupe: a repeated sentence is `activity`, not a `stall_signal`).
- **The live-task set is the loop's; which words open and close a task are the
  classifier's.** `task_bookkeeping` carries the id that entered or left it.
- **What is not a runtime's is shared, not copied:** `apiRetryLine` is the
  loop's; the shell rewrite, failed-output diagnosis and line deltas are
  `lib/progress/tool_rows.ts`; the field caps and heartbeat limiter are
  `lib/progress/adapter_common.ts`. Each takes facts a translator extracted,
  never a message.

`lib/run_loop.replay.test.ts` pins the loop's whole transcript over both probe
recordings (`test/fixtures/probe*.loop.ndjson`).
