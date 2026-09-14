# AGENTS.md — runners/

One-shot / job images (not long-lived services). Run to completion in a pod.

**Status:** `remote-worker/` is the coding-agent runner — a TS Claude Agent SDK
one-shot pod that provisions a workspace and runs the Agent SDK against it.
Skills are **authored in `<repo>/skills/`, not here** (`skills/AGENTS.md` has
the authoring rules) and **delivered by the BFF, not here either**: a run reads
the `.claude/skills/` mirror in its own clone. What this package owns is
consuming that mirror correctly — the always-on workflow, the allowlist, and the
playground's stand-in for the BFF write. The dev flow bind-mounts the library
into the runner pod at `/app/skills` for live skill edits (see
`deployments/scripts/setup-k3d.sh`), which is what the playground mirrors from.

## Conventions

- One entry point per pod (`src/oneshot.ts`); everything reachable from it.
- **Never put a credential in a git URL or in argv.** All clones go through
  `lib/git_clone.ts` — an authenticated URL leaks into `child_process` error
  messages (which the BFF forwards to the console build log), into `ps`, and into
  `.git/config`. Rationale inline in `git_clone.ts`; the BFF keeps a shape-based
  second line of defense in `delivery/codingagent/redact.go`.
- **Git credentials — two modes, one helper value in `.git/config`.**
  When `GITHUB_TOKEN` / `GH_TOKEN` is set (cloud Jobs mount the org PAT),
  `workspace.ts` installs `gh auth git-credential` (the same helper
  `gh auth setup-git` uses), pinned to the **real** `gh` absolute path so the
  `.aep/gh` wrapper cannot intercept. Clone and push share that path; they do
  **not** call `credentials/refresh`. When those env vars are absent, every
  authenticated git op goes through `lib/credhelper.ts` → refresh (clone via
  `git -c`, then the same script installed durably). No GIT_ASKPASS, no token
  in argv or URL. Don't add a third path. Changes to the generated refresh
  scripts must keep `credhelper.test.ts` green — it drives them with real `git`.
- Runner `console.*` is a **user-facing** channel, and it shares the file
  descriptor the NDJSON progress feed writes to. `installConsoleScrubber()`
  converts every call into a scrubbed `notice` run event, so
  the feed stays parseable NDJSON end to end; don't bypass it by writing to
  `process.stdout` directly. The BFF still wraps any non-NDJSON pod line into a
  build-log event, but that is now a safety net, not the normal path.
  **Redaction needs the credential ENROLLED, not just the console wrapped.**
  `installLogRedaction()` does both in one call so every entrypoint gets both,
  enrolling from `lib/credential_env.ts` — which MIRRORS the Go dispatch
  constants with nothing mechanical between them, so a credential added there
  is added here too. One the scrubber cannot enroll is reported by name rather
  than dropped in silence. Rationale, and the credhelper path this cannot
  reach: ADR-0002 decision 19.
- **The progress contract is RUN EVENTS v2, and it is GENERATED, not written
  here.** `RunEvent` lives in `packages/contracts/api/v1/openapi.yaml` and
  reaches this package through `openapi-typescript` (see the generated-types
  bullet below); `lib/progress/emitter.ts` re-exports it and owns only what the
  contract does not carry — the envelope (`v: 2`, `seq`, `ts`), the default
  author of a line, and the scrubber walk. The hand-kept `lib/progress/schema.ts`
  and its Go twin are gone: one document, three generated consumers. Changing
  the feed is therefore a contract change first (`make gen-api`, then `make gen`
  for this package), never an edit here.

  Four rules hold across the whole feed, and each is load-bearing:

  - **Attribution is ONE field.** Every event carries `agentId`; the lead is the
    literal string `lead`. No consumer infers an author from an absence, from a
    tool name, or from anything else. v1's `emitter` / `emitterId` /
    `emitterLabel` are gone with the tree they could not describe.
  - **An agent's life is DECLARED.** `agent_started` / `agent_progress` /
    `agent_settled` come from the runtime's own `task_*` messages, so depth, the
    parent and the report are recorded rather than guessed. v1 had no "an agent
    started" event at all, which is why a depth-2 child was flattened onto its
    parent and no surface could draw a tree.
  - **`turn_ended` is not `run_settled`.** A `result` message is one turn
    ending; the run settles when the stream closes. See the loop bullet below.
  - **Heartbeats are bounded and are never progress.** At most one per agent per
    10s, only while a tool or a model turn is in flight, and the watchdog's idle
    clock ignores them exactly as it ignores retries. A heartbeat that reset that
    clock would hide the stall it exists to report — and the rule has to hold for
    the ones the rate limiter DROPS too, which is why `run_loop.ts` routes those
    messages by TYPE rather than by whether an event came back.

  `notice` carries a CLOSED `code` for the conditions a consumer branches on
  (`api_retry`, `compaction`, `refusal`, `rate_limit`, `permission_denied`,
  `terminated`, `workspace_guard`, the eight dark-zone conditions, …) and a
  code-LESS variant for prose a reader reads and nothing branches on: the
  runner's own `console.*` output, the watchdog's sentence, a dangling skill pin.
  v2 has no `phase` and no `log` kind — v1's eight phase ids became notice CODES,
  and the two the runner raises (`workspace_provisioning`, `workspace_ready`,
  `lib/progress/lifecycle.ts`) carry the code and no prose. If a surface ever
  needs to branch on one of the remaining prose lines, the answer is another code
  in the contract, not a field invented here.

  The WORDING is nobody's here either: `@aep/progress-view` renders every line
  for both the console and the playground, so an event without a case there
  reaches a user as a blank row or a raw field dump. Decisions and the SDK's
  measured capabilities are in
  `remote-worker/design/decisions/ADR-0002-run-observability.md`; read it before
  changing what a line says, because several of its entries are corrections of
  the obvious-looking choice — and its v2 amendment records which of them the
  run-events cutover reverses.
- **`runtime/port.ts` is the WHOLE runtime-specific surface, and `lib/runner.ts`
  depends on nothing else about a runtime.** A `Runtime` answers three things —
  its name, its tool glossary, and `start(prompt, policy)` — and `RuntimePolicy`
  states the platform's rules for a coding run in vocabulary no runtime owns:
  where authored files may land, what a WebSearch query may contain, which skills
  are reachable, which CAPABILITY CLASSES are denied, which model to bill. An
  adapter enforces every clause through whatever mechanism its runtime has:
  `runtime/claude/runtime.ts` turns the guards into `PreToolUse` hooks, the
  capability classes into `disallowedTools` (`runtime/claude/tools.ts`), and the
  MCP policy into an `http` server behind a loopback auth proxy, because this
  SDK's MCP config only accepts a static header. The test for whether something
  belongs in the port is whether a second runtime would write it the same way; if
  it names a tool, a hook or an SDK option, it does not.
  **There is exactly ONE adapter**, and `runtime/registry.ts` refuses `opencode`
  by name with the reason — three spikes are owed (pre-dispatch tool/permission
  parity, whether the stream declares an agent's id/depth/parent, and how usage
  is reported for cost stamping) and each unanswered one fails SILENTLY: an
  unenforced guard, an inferred tree, a blanked cost. A seam that says "not
  implemented" is the deliverable; a stub would be a claim.
  Every deviation from the design's sketch is recorded at its field in
  `port.ts` and argued in
  `remote-worker/design/decisions/ADR-0012-the-runtime-is-a-port-with-one-adapter.md`
  — read it before reshaping the interface, because the largest one (the session
  exposes `stream` + `translate`, not a flat `events()`) is a measured constraint
  of the WATCHDOG's contract, not a preference.
- **`lib/progress/claude_adapter.ts` is the TRANSLATION half of that adapter.** SDK messages in, run events v2 out. Runtime names must not reach
  a consumer's logic: `tool` carries the SDK's own tool name because that is what
  a row prints, but fan-out is `agent_started`, never "a `tool_result` whose tool
  is called `Agent`". It is a per-run factory — the agent registry, the in-flight
  calls and the heartbeat clocks describe ONE run, and two runs sharing them
  would mislabel lines rather than merely lose detail. A second runtime is a
  second adapter and nothing else.
- **The run settles when the SDK stream CLOSES, and the feed gets exactly one
  `result` line.** `remote-worker/src/lib/run_loop.ts` owns both, and both are
  counter-intuitive. A `result` message is one TURN ending: a lead can launch a
  background subagent and end its turn while that subagent works, so returning
  there kills the pod mid-flight (measured —
  `remote-worker/test/fixtures/probe2-lead-ends-early.jsonl`, first `result` at
  message 16 of 40, subagent steps at 18-25). The loop keeps reading and writes
  `run_settled` when the iterator closes; the exit code follows that same
  settle. The adapter emits a `turn_ended` per SDK `result` — informational, on
  the feed where it happened — and the loop remembers the newest one so the
  settle can carry its outcome and its usage. So **do not emit a `run_settled`
  from anywhere else**: several would settle one run several times, and every
  consumer treats a settle as terminal — `buildCrew` in
  `packages/progress-view/src/crew.ts` settles every agent it never heard close
  on the run's settle, so a second one describes a run that had not ended.
  Anything that has to end a RUNNING run early states its reason through the
  loop's own seam instead (`createRunTerminator`, `run_loop.ts`): the deadline
  and a fatal MCP auth failure both trip it, and the loop stops the tasks still
  live, names the cause in a `terminated` notice and writes the one settle. It
  shipped the other way once — the MCP policy's `onFatal` emitted its own settle
  while the loop was still reading, so a fatal put two on one feed.
  **The one carve-out is a failure BEFORE the loop exists**: `oneshot.ts` and
  `local.ts` settle their own pre-flight failures (a workspace that would not
  provision, a validation context that would not load, a mirror with no workflow
  skill) and they may, because `consumeRun` is never reached on that path — the
  run really did end there and there is no second settle to collide with. That
  carve-out does not travel: copied anywhere the loop is already reading, it is
  the double settle again. Usage is NOT summable across turns —
  the runtime reports it cumulatively, so the last turn's number IS the run's
  total. The loop takes its stream as an `AsyncIterable` exactly
  so a recording can be replayed through it: when a change turns on what the SDK
  does, add a fixture rather than a hand-written mock — both probes in
  `remote-worker/test/fixtures/` are replayed through it in `run_loop.test.ts`.
  `AEP_RUN_DEADLINE_SECONDS` bounds a run that never ends — it stops the tasks
  still live and settles as a failure. The dispatcher stamps it with the SAME
  number it puts on the Job's `activeDeadlineSeconds`, so the pod's own budget
  and the cluster's backstop cannot disagree; the runner subtracts its own
  margin, which is not the dispatcher's to know. Unset still means no guard,
  which is what the playground runs under.
- **API retries are on the feed for every run; the rest of the diagnostics are
  developer-only files.** A stalled model turn used to be reported as bare
  silence. The SDK emits `system`/`api_retry` for every retryable failure and
  the translator was discarding it, so `progress/diagnostics.ts` reads it into a
  `warn` notice and the watchdog names it in its own. Ungated on purpose: a healthy
  run emits nothing, the `error` field is a closed enum (no prompt or credential
  can ride it into a console build log), and overload is load-dependent so a flag
  would be off during every incident. **A retry must never reach
  `watchdog.observe`** — it is the absence of progress, and resetting the idle
  clock hides the stall it explains; the same now holds for heartbeats. **A
  running agent is known from `agent_started`, and closed by `agent_settled`** —
  the adapter emits no `tool_use` for a fan-out call, so a watchdog that expected
  one reported "no tool in flight" for the whole of an agent's run (it did,
  through a ten-minute live stall). v1 had to register an agent from the first
  LINE it produced, which was a few seconds late and the only start that stream
  carried; v2 has the runtime's own declaration. **A failed spawn prints its
  error text as an `error`-level notice**, because that text is the last copy of
  the reason: the agent's transcript is not on the feed and `claude.log` dies
  with the pod. `debugFile`, `stderr`,
  `includePartialMessages` and the reasoning pair (`thinking` +
  `forwardSubagentText`) are the opposite call: on for every playground run,
  off in a pod unless `AEP_RUNNER_DEBUG=1`, and they land in files beside
  `claude.log` rather than on the feed — nothing collects a pod's files and the
  debug log holds prompt text. Streaming frames reach neither the feed nor
  `claude.log`. **The reasoning pair only works as a pair**: without a
  `thinking` display the blocks arrive signed and empty, and without
  `forwardSubagentText` the subagents forward none at all — which is why a
  transcript could show 120 subagent tool calls and not one word of why. Adding
  either alone re-creates a log that says reasoning happened without saying what
  it was. ADR-0002 decisions 14–16 have the measurements, including why stderr
  is *not* where retry detail lives.
- **A validation run keeps its own issue's status line, and the platform writes
  it.** `lib/validation_status_line.ts` is a WATCHER on
  `RuntimePolicy.observe.toolUse` beside the per-criterion one, sharing its
  `ValidationProgressState` so a row and the line above it cannot disagree (the
  two are fanned out in `runner.ts`, rows first, so neither can swallow the
  other's call). Two things make it unlike every other watcher here.
  It performs **I/O on the agent's path** — an awaited `gh issue comment`
  through the REAL `gh` (`resolveRealGhPath`, never the `.aep/gh` wrapper) —
  because the whole value is that the line lands BEFORE the silence it explains;
  a detached post during a twenty-minute exploration could land after it, and
  `RuntimeObservers.toolUse` is awaited for exactly this one caller (ADR-0012).
  And it reads the **outcome** as well as the call, through the same
  `observe.toolOutcome` seam the rows settle on, because the report generator
  FAILING is what puts a run into its repair mode. A failure is warned and swallowed: two hours of work
  must never die because it could not be watched. The issue number arrives as
  `AEP_VALIDATION_ISSUE`, stamped by the BFF — nothing else in the pod answers
  "which issue", since `AEP_TASK_ID` is the cycle's uuid and the number reaches
  the agent only as prose inside `AEP_PROMPT`. Rungs are one-way and the repair
  mode absorbs the exit-2 loop, which is what keeps a lapping run to six lines
  instead of three per criterion; `design/decisions/ADR-0011-the-platform-writes-a-validation-runs-status-line.md`
  has the measurements, including the p44 run that produced two wrong lines
  before either rule existed.
- **Fan-out is NOT forced into the foreground any more, and the hook that did it
  is deleted.** `lib/fanout_foreground.ts` rewrote `run_in_background` to `false`
  on every `Agent`/`Task` call, for two measured reasons. The first — that a
  backgrounded subagent's messages are not forwarded at all — was true of SDK
  0.3.220 and is **not** true of 0.3.247: both recordings in
  `remote-worker/test/fixtures/` show a backgrounded agent's steps arriving
  attributed, with its depth, its parent and its report. The second — that a
  detached agent lets the session end while its children work — was real and is
  fixed where it belonged, in the loop that treated the first `result` as the
  run ending. Neither reason survives, so the rewrite would now cost the
  concurrency it was never buying: **backgrounding is what lets a lead keep
  working while its builders build**, and the `aep` skill governs the shape.
  The regression pin moved with it — `run_loop.test.ts` replays probe 1 and
  asserts a backgrounded spawn arrives as `agent_started {background: true}`
  followed by its own attributed steps. Read ADR-0002 decision 13 and its
  amendments before reintroducing any of this. The shape itself is the skill's,
  and it is now background-by-default:
  `ADR-0014-fan-out-is-backgrounded-by-default.md` is that decision, and the
  glossary below is what lets the skill state it without naming a runtime.
- **Authored files land in the project.** `lib/workspace_guard.ts` is a
  `PreToolUse` hook that denies `Write`/`Edit`/`NotebookEdit` outside the
  workspace, and `promptWithProjectRoot` (`lib/runner.ts`) states the absolute
  root in the prompt — the runner is the only layer that knows it, since the two
  prompt builders sit either side of a language boundary and the path is decided
  after `provisionWorkspace`. Both exist because a run inferred that the
  skills-plugin directory's parent was the project root and built a whole
  component there, green. Writes are allowed outside the project in exactly two
  trees: the temp directory, and **any dot-directory under `$HOME`** — one rule,
  because every toolchain hides its cache in one (`.ballerina`, `.npm`, `.m2`,
  `.cargo`) and this module has no business tracking which stacks the image
  ships. The earlier version listed three, which silently contradicts the next
  stack skill added. A visible directory under `$HOME` stays denied, so a sibling
  checkout is still caught. Reads are deliberately NOT gated: a skill's
  `references/` live outside the project by construction, and the agent must be
  able to read the toolchain's own installation for a library's real signature.
  Bash is not gated either; a build writes where it writes, and the pod is the
  containment boundary. The guard catches the one expensive mistake, it is not a
  sandbox.
- **`allowedTools` restricts nothing here.** `bypassPermissions` +
  `allowDangerouslySkipPermissions` allow every harness tool regardless, so
  `BASE_ALLOWED_TOOLS` documents intent while the DENY list is the boundary
  that holds. Both live in `runtime/claude/tools.ts`, and the deny list is
  derived: the port states CAPABILITY CLASSES (`interactive_prompt`,
  `scheduling`, `durable_session`, `peer_messaging`, `artifact_publishing`) and
  that file maps each to this runtime's names. There are no runtime-neutral tool
  names to write — a second runtime shares none of these sixteen — but the two
  lists share the REASON each entry is on them, which is the sentence below. Keep the surface that assumes an interactive user, a scheduler, a
  durable session or a peer to talk to (schedulers, cron, prompts, worktrees,
  messaging) in the deny list: a one-shot pod has none of those, and a
  reachable-but-useless tool is somewhere a run will spend a turn.
  **The whole TASK surface is deliberately allowed**, and it took two
  corrections to get there. `TaskOutput`/`TaskStop` came off first: a lead that
  backgrounds work has to be able to wait on it and to stop one that runs away,
  and denying them is what left a run reaching for `ScheduleWakeup` instead
  (`remote-worker/test/fixtures/probe1-background-fanout.jsonl` shows the healthy
  shape: three `TaskOutput` calls, one per agent).
  `TaskCreate`/`TaskUpdate`/`TaskGet`/`TaskList` followed. They were denied as
  "a durable board's surface, and this pod has no board", and that had the
  audience wrong: the board is not for a next session, it is for the person
  watching this one. A lead's plan is the only statement of intent a run
  produces, and v2 puts it on the feed as `work_item {source: "plan"}` rows a
  console folds by item — so the plan being true is worth more than the turn it
  costs. Corollary: a typo in `BASE_ALLOWED_TOOLS` cannot fail loudly — it named
  `Task` for a whole SDK generation after the tool became `Agent`.
- **`settingSources` is `["project"]`, and that is load-bearing.** It lives in
  `runtime/claude/runtime.ts` now, with the other two invariants that are
  conditions of running this platform's workload rather than policy anyone
  decides per run (`strictMcpConfig`, and `bypassPermissions` +
  `allowDangerouslySkipPermissions`). The BFF
  mirrors the org's coding-relevant skills into the project clone at
  `.claude/skills/`, and the SDK only discovers them if the project source is
  admitted — its `skills:` option is an ALLOWLIST over discovered skills, not a
  loader, so a name matching nothing is dropped in silence. That shipped once:
  the run reported success while the agent compensated by grepping `SKILL.md`
  out of the tree. `AGENT_SETTING_SOURCES` is exported and
  pinned by a test so a revert to `[]` fails there instead of in a build, and
  `startCodingRun` warns when the `init` message's resolved list is missing
  something we asked for (`skills_preload_check.ts`). 'user' and 'local' stay
  out — a developer's `~/.claude` has no place in a container run. The MCP
  isolation that `[]` used to give for free is now explicit: `strictMcpConfig`
  keeps a project's own `.mcp.json` from declaring servers into a run.
- **`skills:` is an allowlist, and it preloads nothing.** Both halves are
  measured, not assumed. A mirrored skill absent from the array is *rejected* by
  the Skill tool, so a CODING run lists the WHOLE mirror
  (`listMirroredSkills`) — the BFF already decided what this build may use, and
  omitting the unpinned copies would leave them as inert files on disk. A
  VALIDATION run lists `onDemandSkills` instead: it builds nothing, so the stack
  skills in the mirror are not its to reach for, and one named skill is a
  narrower statement than the whole checkout. Passing `[]` there — which shipped
  — is not "defer the load", it is "no load is possible". And
  membership only buys a name and a description in the catalog: the body arrives
  when the model invokes the skill. So a pin, which asserts the guidance IS
  needed for this work, appends that body to the system prompt
  (`readSkillBodies`) instead of trusting the model to go looking. The comment
  claiming `skills:` "injects full bodies at startup" was wrong for as long as it
  existed, through the earlier `aep-task-skills` plugin too — an agent given a
  listed skill cannot state a codeword from its body until it calls the tool.
- **The RUNTIME and the MODEL are an organization setting, and they arrive as
  env.** `AEP_AGENT_RUNTIME` and `AEP_AGENT_MODEL` are stamped onto the Workload
  by `delivery/codingagent`, copied from the org's `/config` `codingAgent`
  section — copied, not referenced, so a change applies from the NEXT cycle and a
  run in flight keeps the model its usage lines were billed against. Unset means
  the platform defaults (`claude-code`, `claude-sonnet-5`), which is what every
  dispatch carried before the setting existed and what the playground still runs
  under. An unrecognised runtime is an error, never a silent fallback: running
  the one we do have would bill an org for a runtime it did not choose. The model
  is no longer a literal in `runner.ts`.
- Self-contained: all agent and SDK-specific wiring lives here.
- **The runner's contract types are GENERATED and DELIBERATELY NOT COMMITTED.**
  `pnpm --filter remote-worker gen` (wired into root `make gen` via turbo) runs
  `openapi-typescript` over `packages/contracts/api/v1/openapi.yaml` — the same
  committed document `aep-api` and the console generate from — into
  `remote-worker/src/generated/aep-api.d.ts`. The root `.gitignore` rule
  `generated/` keeps it out of git, and that is correct, not an oversight:
  the image runs `npx tsx src/oneshot.ts`, there is no `tsc` in the
  image, and `tsx` erases types — so a **type-only** import
  (`import type { components } from "../generated/aep-api"`) resolves at
  workspace typecheck and vanishes before the pod ever runs. Do NOT "fix" this
  by committing the file (it would go stale against the contract, which is the
  exact failure the gitignore rule exists to prevent), and do NOT create a
  runtime (value) import of the generated module — `import { ... }` without
  `type` survives erasure and the pod would crash on a missing file.
  `remote-worker/turbo.json` overrides `gen.inputs` to name the contract
  because the file it reads lives two packages up, the same override
  `apps/console` carries.

  **`openapi-typescript` is deliberately NOT a dependency of this package** —
  the script runs the repo ROOT's copy, which pnpm puts on a workspace script's
  PATH. Declaring it here breaks the image: this package pins
  `typescript@^6`, openapi-typescript's peer is `typescript@^5.x`, and npm (which
  is strict about peers where pnpm only warns) then refuses to resolve, so
  `npm install --package-lock-only` errors and the Dockerfile's `npm ci` has no
  lockfile to read. Keeping it at the root also keeps a host-only codegen tool
  out of the pod image entirely, since `npm ci` there does not omit dev
  dependencies.
- **Skills scope is stated by the caller, never read off `AEP_COMPONENT_NAME`.**
  A milestone Job carries a sentinel there (`aep-milestone`), so an
  implementation run resolves the union of `skillsPinned` across every
  `specs/design/components/*/design.json`; a validation run applies no design
  skills at all. The local harness (`local.ts`) carries its own sentinel
  (`aep-local-milestone`) for the same reason: a playground coding run works
  the whole project, same as the milestone loop, and may touch several
  components — there is no single one to name.
- **There are NO plugins, and the mirror is the only skill source.** The runner
  once loaded two — one it assembled from the library, one it materialised per
  task — and both are gone. `aep`, `aep-validation` and `playwright-cli` are
  library skills carrying `audience: [coding]`, so the BFF mirrors them into the
  project repo exactly like `go`, and a coding session reads one directory. What
  reaches a build is therefore decided in one place, by the BFF: `design`'s
  description cannot appear in a coding session's catalog because
  `audience: [design]` keeps it out of the mirror, not because the runner filters
  a library. ADR:
  `remote-worker/design/decisions/ADR-0005-the-workflow-rides-the-project-mirror.md`.
- **The always-on set is the runner's, not the design's.** `alwaysOnSkills`
  (`lib/runner.ts`) names `aep` for every run and `aep-validation` for a
  validation task; `requireWorkflowBodies` reads those bodies out of the mirror
  and appends them to the `claude_code` preset. Everything else a component needs
  is a `skillsPinned` entry someone put in a `design.json` — but no design decides
  whether a coding run follows the coding workflow. `playwright-cli` is
  deliberately NOT always-on: `aep-validation` names it, and mechanics a run may
  not reach for should cost a load, not every turn. **That decision only works
  in pairs** — `onDemandSkills` (same file) must then ALLOW it, because `skills:`
  gates the Skill tool and a skill in neither list is unreachable rather than
  deferred. It was in neither for three weeks: validation runs looked healthy
  (their workflow arrives as prompt text, not through the tool) while every
  `Skill playwright-cli` call was rejected and the agent grepped the mirror's
  files by hand.
- **The workflow names tool ROLES; `lib/tool_glossary.ts` binds them.** The `aep`
  skill says "the fan-out tool", "the wait tool", "the task list" rather than
  `Agent`, `TaskOutput`, `TaskCreate`, because one authored library is shared by
  every org and a body naming this runtime's tools would mis-steer any other one
  — silently, since prose cannot fail at startup the way a missing overlay anchor
  does. `systemPromptAppend` (`lib/runner.ts`) is the order that makes it work:
  the workflow body, then the pinned skill bodies, then the glossary LAST,
  because the skill points at it by position ("the tool glossary at the end of
  your instructions"). Append nothing after it. A second runtime is one more
  entry in `GLOSSARIES` and nothing else — this is not the runtime port, which is
  the seam `progress/claude_adapter.ts` sits on. `make workflow-skill` prints the
  glossary after the composed body in both modes, since the roles do not resolve
  without it. ADR-0014.
- **A mirror with no workflow skill is FATAL.** `requireWorkflowBodies` throws and
  both entrypoints report a failed run. Every other skill degrades — a dangling
  pin warns and the build continues — because missing guidance costs quality and
  aborting costs the whole build. The workflow is the exception: a session without
  it improvises a procedure and reports success, which is invisible from outside.
  The mirror's writes are best-effort by design (they may not fail a creation,
  publish or dispatch), so this is where that becomes visible. **Do not add an
  image fallback**: two sources drift, and the fallback would silently discard an
  org's own edit to the skill. The check sits in `startCodingRun`, so no new
  entrypoint can start a procedure-less session.
- **Anything a skill must invoke by absolute path reads `$AEP_SKILLS_DIR`**, now
  `<workspace>/.claude/skills`. The runner stamps it (`lib/runner.ts`) because it
  is still the only layer that knows the value. `aep-validation` runs the
  platform's report generator through it, and the component contract a lead hands
  to fan-out subagents (`contractReferencePath`) resolves the same way. A
  hardcoded path is wrong somewhere — it was, and it named `/app/plugin`.
- **`lib/workflow_skill.ts` composes ONE file**: `skills/aep/SKILL.md` for a mode.
  Production mirrors the authored trunk verbatim; local mode applies
  `skills/aep/overlays/local.md`, and `local_skill_mirror.ts` does it **while
  writing the mirror**, which is what keeps it unskippable. Every writer filters
  `overlays/` out — `loadLibrary` never seeds it, so no org repo has one for the
  BFF to copy — because the `aep` skill lets the agent read its own directory and
  an overlay beside `SKILL.md` is a second procedure it can find.
  `make workflow-skill` prints either mode without spawning a session.
- **The library arrives as a BuildKit named context**
  (`--build-context skills=<repo>/skills` → `COPY --from=skills . /app/skills`),
  the same mechanism `aep-api` uses. Add it to any new build path or the
  playground has no library to mirror: `build-runner.sh`, `release.yml`'s matrix
  row, and `local/run-local.sh` all pass it. A dispatched run does not read it —
  its skills come from the clone — but `local.ts` does.
- **The `bal library` tool is BUILT BY THE IMAGE and installed, not bundled onto
  `PATH`.** It is what the `ballerina` skill drives by name (`bal library
  overview <org/name>`), and it is a Ballerina CLI tool: the image installs it
  into the `aep` user's local bala repository and `bal` dispatches `library` to
  it, so there is no command and no `PATH` entry. The image's FIRST STAGE
  compiles it from `packages/bal-library-tool`, reached as the
  `bal-library-tool` named build context — so there is no artifact to refresh
  and a build cannot use a tool that is not this commit's (ADR-0008; a committed
  copy used to live here and went stale silently, because its version string
  never moved). That stage needs a token that can read ballerina-platform's
  GitHub Packages, passed as a BuildKit **secret** and never a build arg, since
  the release workflow publishes builder stages to a public buildcache. Every
  build path has to pass both the context and the secret: `build-runner.sh`,
  `release.yml`'s matrix row, and `local/run-local.sh`.
  The install runs the tool's OWN installer so
  the bala is stamped with this image's pinned distribution — `bal` rejects a
  tool stamped newer than the distribution running it. Tools a skill invokes by
  name belong here rather than in the skill directory: nothing but prose then
  reaches an org's editable skills repo. See
  `remote-worker/design/decisions/ADR-0008-the-bal-library-tool-is-built-in-the-image.md`.
- **This package is inside the eslint gate, and was not until 2026-09-07.**
  `make lint` is `turbo run lint`, which runs a package's own `lint` script — and
  `remote-worker` had none, so the most safety-critical TypeScript in the repo
  (credential helpers, the write guard, the DLP and SSRF hooks, the progress
  feed) was the one package nothing linted. Three findings had accumulated
  unnoticed, one of them a useless escape inside a **secret-redaction regex**,
  which is exactly the place a silent character-class mistake costs the most.
  Keep the `lint` script; a new entry point or module has to pass it.
- **The image installs from `remote-worker/package-lock.json`, not from
  `pnpm-lock.yaml`.** Two lockfiles, one `package.json`: pnpm's covers the
  workspace (tests, typecheck, the playground), npm's is what `npm ci` in the
  Dockerfile reads. Bump a dependency and BOTH have to move —
  `npm install --package-lock-only` in this directory — or the image build fails
  on an out-of-sync `npm ci`, which is the loud outcome. The quiet one is worse:
  a range that still resolves leaves the pod running a version the tests never
  saw.
- **The image states what the environment IS, so no agent has to discover it.**
  Two entries earn their place there rather than in a skill or a prompt.
  `AGENT_BROWSER_ARGS=--no-sandbox` (Dockerfile): a pod has no usable chromium
  sandbox — no setuid helper, and the runtime's seccomp profile denies the
  unprivileged user namespace the zygote falls back to — so without it the first
  browser command of a walk fails and the agent spends failed tasks guessing.
  The pod is the boundary that holds; the argument for why dropping chromium's
  own is acceptable is at the variable. And `remote-worker/docker-entrypoint.sh`,
  the image's ENTRYPOINT, which sets `ulimit -c 0` (soft AND hard, so no
  descendant can raise it) before exec'ing the CMD: a `bal build` JVM or a
  chromium that crashes used to drop a `core` of tens of megabytes into the
  cloned repository, untracked and one `git add -A` from a customer's PR. It has
  to be the image because Kubernetes has no `ulimit` field and Node cannot call
  `setrlimit`. Any new way of starting the container goes THROUGH that wrapper —
  the playground's `--entrypoint` names it (`playground/src/engine/coding-run.ts`)
  rather than `npx`, which is how it skipped the limits for a while.
  What the image CANNOT state is `/dev/shm`, which is the caller's: this
  Chromium aborts on the 64Mi a pod gets by default, so all three run paths size
  it to 1 GiB — `--shm-size=1g` locally and in the playground, a bounded
  `medium: Memory` emptyDir in the Job. ADR-0013 has the two properties that are
  easy to get wrong (it is charged to `memoryLimit`, and unbounded it is sized
  from the NODE's memory) and why `--disable-dev-shm-usage` was rejected.
  The `core` guard has a second half in the RUNNER: `installCrashArtefactExclude`
  (`lib/workspace.ts`) writes the crash-artefact patterns into the clone's
  `.git/info/exclude` — per-clone, never committed, so no platform concern
  reaches a customer's `.gitignore`. It is not redundant with the rlimit (it
  covers the JVM's own `hs_err_pid*.log`, which no rlimit suppresses), and
  neither is redundant with the patterns `skills/aep/SKILL.md` names — that is
  the only copy a reader meets when they wonder why `core` is not in
  `git status`.
- **One image**, `remote-worker/Dockerfile`, serves BOTH task kinds
  (`AEP_TASK_KIND=implementation` and `=validation`). It is Debian-based
  because Playwright's browsers are glibc-linked; do not reintroduce a second,
  slimmer image without moving the Helm/compose/release/`AGENT_RUNNER_IMAGE`
  consumers with it. Build + k3d-import it locally with `make build-runner`.
  Full `deployments/scripts/setup.sh` pre-builds it in the background (off the
  critical path) and imports it in `setup-aep.sh`; `PREBUILD_RUNNER=0` reverts
  to a serial build. The build is skipped when the tag exists, so use
  `FORCE=1 make build-runner` after changing the Dockerfile or `src/`.
- **The imported tag is pinned in containerd** — `build-runner.sh` calls
  `pin_node_image` (`deployments/scripts/utils.sh`) after a successful
  `k3d image import`. `aep-runner:dev` is local-only, so there is no registry to
  re-pull from, and it sits idle between dispatches: kubelet's imageGCManager
  collects least-recently-used images first (it sorts `byLastUsedAndDetected`, not
  by size) once the node's image filesystem crosses its high threshold (85%,
  freeing down to 80%), so an idle runner tag goes early and its size means one
  eviction covers much of the target. That leaves the next dispatch in
  `ImagePullBackOff` with nothing to recover from. The same helper
  covers the other local-only imports (`thunder-app-operator:local`, the patched
  RCA image). It doubles as import verification: an image in no node's containerd
  means the import silently did not land. Verify a pin from the host with
  `docker exec k3d-openchoreo-server-0 crictl inspecti aep-runner:dev` →
  `"pinned": true` (there is no host-side `crictl`). An import replaces the
  containerd record, so the pin has to live in the import path, not in a manual
  step.
