# ADR-0015 — OpenCode is the second adapter, and it enforces the port with its own mechanisms

**Status:** Accepted

## Context

ADR-0012 left three questions a second runtime had to answer before its adapter
was worth merging: pre-dispatch tool and permission parity, whether the stream
declares an agent's id, depth and parent, and how usage is reported for cost
stamping. For OpenCode (1.18.32, through `@opencode-ai/sdk`) all three answers
are yes, with corrections. The recordings are this package's fixtures
(`test/fixtures/opencode-*.jsonl`, README there). OpenCode's failure mode is
silence: several of its mechanisms degrade a run without an error on the wire.

## Decision

`runtime/opencode/` implements the port, and the registry builds it for
`AEP_AGENT_RUNTIME=opencode`. It drives OpenCode's headless SERVER through the
SDK's typed client, not `opencode run --format json`, whose JSON mode drops
child attribution, permissions and the todo list, and exits on the root's first
idle.

### What it enforces, and with which mechanism

| Policy clause | Mechanism | Why |
|---|---|---|
| `workspace` | the server's cwd and the client's `directory` | every tool the server spawns inherits both |
| `env` | the server's environment (`childEnvironment`), plus `OPENCODE_DISABLE_PROJECT_CONFIG`, `_AUTOUPDATE`, `_MODELS_FETCH`, `_LSP_DOWNLOAD` | the checkout's own config never reaches a run, and a pod starts offline; set here, the one place every run path passes through |
| `model` | config `model`, `small_model` and the one `general` subagent, `anthropic/`-spelled; usage reported back in the platform's spelling | one model per run (below) |
| `write` | the **aep-guard plugin**'s `tool.execute.before` on `edit`/`write`/`apply_patch`, the only enforcer | it runs before any path permission is asked, so the agent reads the platform's own sentence |
| `webSearch`, `webFetch` | the same plugin, fed the run's staged-secret values through a 0600 file | a closure cannot cross into the plugin's process, and the config is an env var any child reads |
| `deniedCapabilities` | permission denies from `runtime/opencode/tools.ts` (`question`; the other four classes have no OpenCode tool), `share: "disabled"` | the Claude adapter's reasons, OpenCode's names |
| `skills.allow` | `permission.skill`, an allowlist written `"*": "deny"` FIRST | rules are last match wins, and a trailing `*` deny hides the whole tool |
| `skills.preloadBodies` | an instructions file named in `instructions`, glossary last, kept to the lead by the plugin (below) | the file keeps its place in the lead's prompt |
| `skills.dir` | discovered natively from `.claude/skills/` | the one directory both runtimes read (below) |
| `mcp` | a `remote` MCP entry behind the SAME loopback auth proxy Claude Code uses (`lib/mcp_auth_proxy.ts`), `oauth: false` | OpenCode's headers are static too, and a 401 must not start its own OAuth discovery |
| `debug`, `logDir` | `--log-level=DEBUG --print-logs` into `<logDir>/opencode.stderr`, the config beside it as `opencode.config.json`; on every run, the plugin's `session-context.jsonl` | developer files, never the feed |

The rest of the config is not a policy clause but a condition of the workload:
`default_agent: aep` (the built-in `build` prompt must not compete with the
workflow), `build`/`plan`/`explore`/`scout` disabled (a task naming one fails),
`subagent_depth: 3` (the default of 1 forbids the builder → walker nesting the
skill allows), `snapshot: false` (no per-step git snapshot of the workspace), no
LSP (nothing serves it in a pod), the subagent without `todowrite` (the plan is
the lead's), and never `ask` (server mode has nobody to answer, so the runtime
rejects every ask).

**`external_directory` is `allow`.** That permission gates every path outside
the project for reads and bash as well as writes, with no read/write split, so
`deny` refused the platform's own context file; the guard plugin is the one write
gate, as `lib/workspace_guard.ts` is on Claude Code.

### One skills directory for both runtimes

The project's skills stay in `.claude/skills/`: OpenCode scans it natively, and
Claude Code scans nothing else, so a neutral `.agents/skills/` would need a
committed symlink in every generated repo. The image keeps `~/.claude` and
`~/.agents` empty, because OpenCode also scans both under the home directory.

The guard plugin is authored in `src/runtime/opencode/plugin/` and bundled from
the same modules the Claude Code hooks are built from (`authoredPathDenial`,
`allowsWriteOutsideProject`, `webSearchDenial`, `webFetchDenial`), so the rule
and the sentence the agent reads cannot drift between runtimes. Its inputs and
ready marker live in a private temp directory, not in `logDir`, which in a pod
is inside the clone the agent commits from.

### The appendix reaches the lead only

On Claude Code the appendix is the main thread's system prompt. OpenCode adds
every `instructions` file to every session's system prompt, so each builder
would read the lead's workflow and fan-out glossary. An agent's `prompt` cannot
carry it instead (it replaces the provider base prompt), and a prompt's
per-message `system` is lost at auto-compaction.

So the file stays the carrier, and the plugin removes its block from the system
prompt of every session whose agent is not `aep`, in
`experimental.chat.system.transform`. That hook is told the session, not the
agent; the agent comes from `chat.message`, and a session not seen there is
treated as a subagent (`plugin/context.ts`). The plugin also records, per
session, the agent, whether the appendix was in its first model call, and each
`skill` call, in the shape the Claude adapter writes (`lib/run_context.ts`).

### Silent failures, asserted at start

Before the prompt is sent, so a refusal costs no model call, `bootOpencode`
proves four things and fails the run with an `error` notice otherwise
(`startup.ts`):

1. **The guard loaded.** A plugin that is not a package directory naming its
   entry is skipped without a log line; the plugin writes a marker at init.
2. **The workflow's tools are visible and `question` is not**, computed by
   applying OpenCode's own hiding rule (`hiddenTools`, a port of its
   `Permission.disabled`) to the `aep` agent's merged rules. A version bump
   re-checks that port.
3. **Fan-out is foreground.** The `task` tool's schema must not carry
   `background`, whose presence means the experimental flag leaked into the pod.
4. **The system transform is live.** The hook is experimental, and without it
   every subagent gets the appendix. A probe prompt on a throwaway session is
   marked by the plugin at the transform and refused at `chat.params`, the next
   hook, before any provider call (`plugin/startup_probe.ts`).

### Fan-out is foreground-only on OpenCode

The platform does not set the experimental background flag. A wave is parallel
`task` calls in one message; they run concurrently and the lead is held until
the slowest returns. Every OpenCode agent is `agent_started {background:
false}`, stated rather than omitted. The OpenCode glossary entry says so in the
lead's terms, so the skill's background-shaped prose needs no edit. The cost is
ADR-0014's idle lead. The close rule below does not hold in background mode: it
closes before the lead's wrap-up turn.

### The stream's end is decided by a rule, not by the bus

OpenCode's bus is the whole server's and does not end. The session's stream ends
when the root session is idle (`session.idle`), no other session is busy, and no
permission is waiting. With foreground fan-out the root cannot idle before its
children, so the rule is a guard rather than the settle itself. `endInput` is a
no-op (the prompt goes out with `promptAsync`); `stopTask` is `session.abort`
on a child session id.

### What the loop is told

The classifier maps the bus onto the port's classes: `session.status {retry}` is
a retry, a permission ask is a `permission_denied` stall signal,
`message.part.delta` and `session.status {busy}` are model waits (each also a
rate-limited `heartbeat {waitingOn: model}`), the root's idle is a turn end, a
child's creation and idle are task bookkeeping. Two port changes came with it:

- **`MessageClass` gained `noise`.** Keep-alives, plugin and catalog
  announcements and file-watcher echoes, as `activity`, would reset the
  watchdog's idle clock for as long as the server stayed up.
- **`ApiRetryInfo.maxRetries` is nullable.** OpenCode's retry status states no
  ceiling, and printing one would state a bound nobody enforces.

The adapter adds two messages of its own: `aep.skills` (what `GET /skill` says
the server discovered, for the preload check; OpenCode has no `init`) and
`aep.tick` (a ten-second clock, because OpenCode sends nothing while a tool
runs; it becomes a tool heartbeat for each call still running).

### Translation

Agent id = session id; parent = `Session.parentID`; label and role from the
spawning `task` part's input. The part's `running` update and the child's
`session.created` arrive in either order, so a spawn is keyed by
`metadata.sessionId` and `agent_started` goes out when the second arrives.
Reports are the child's last assistant text. Line counts come from successful
`write`/`edit` inputs, because every session summary on the wire is zero. A
shell call that ran to a non-zero exit is `completed` on the bus and `ok: false`
on the feed. The plugin's refusal carries a marker in the tool's error text, the
only place the runner sees it, and becomes the same `workspace_guard` notice the
Claude hook raises.

Usage is summed per model across every session, cumulatively, with
`outputTokens = output + reasoning` (the provider bills reasoning as output).
The root's idle carries it on `turn_ended`; a run ended before that (deadline,
fatal) settles with the adapter's running total (`RuntimeSession.usage`).

### One model serves every call, on both runtimes

`RuntimePolicy.model` (`AEP_AGENT_MODEL`) is the only model a run uses: the
lead, every subagent and the runtime's own helper calls, because the org's key
may reach no other and the platform prices only what it has rates for. On
OpenCode it is `model`, `small_model` (so titles and summaries never fall to
OpenCode's own pick) and the one `general` subagent. On Claude Code every alias
(`ANTHROPIC_DEFAULT_SONNET_MODEL`, `_HAIKU_`, `_OPUS_`, `_FABLE_`) and
`CLAUDE_CODE_SUBAGENT_MODEL` are pinned to it (`modelPinEnv`), so the CLI's
helper calls land on it too. The skill and the glossary name no model, so a
fan-out call cannot reach a second one.

## Consequences

- The org's `agents` setting selects the runtime; the dispatcher stamps
  `AEP_AGENT_RUNTIME` and picks the image for it. The image is a second target
  of the same Dockerfile (`runners/AGENTS.md`), published by the release as
  `ghcr.io/wso2/aep/remote-worker-opencode` and pinned into the chart's
  `codingAgentRunner.opencodeImage`. An installation without it does not offer
  OpenCode (aep-api ADR-0028, amendment).
- OpenCode needs `ANTHROPIC_API_KEY`; a Claude subscription token is refused at
  start. Dispatch mounts the org's subscription only on Claude Code (aep-api
  ADR-0036).
- The adapter contract test (`runtime/contract.test.ts`) holds both adapters to
  the same run-event shape on the closest recorded pair (a 3 + 1 fan-out). One
  scripted session with a denied write and a commit, recorded from both
  runtimes, is the fixture still owed; those rows are pinned per adapter.
