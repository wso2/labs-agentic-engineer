# Runner test fixtures

Recorded inputs for the progress translator and the runner loop. They are the
evidence the run-events design was measured on, kept here so a test can replay a
real session instead of a hand-written one, and so a future SDK bump can be
re-measured rather than assumed.

| File | What it is |
|---|---|
| `probe1-background-fanout.jsonl` | Claude Agent SDK 0.3.247, 92 messages. Three subagents launched with `run_in_background: true` in one turn; one of them spawns a depth-2 child. Shows `task_started` with `is_backgrounded` and `spawn_depth`, assistant messages attributed through `parent_tool_use_id`, and `task_notification` carrying `summary` / `output_file` / `usage`. |
| `probe2-lead-ends-early.jsonl` | Same SDK, 40 messages. The lead ends its turn while a background subagent is still running. The first `result` arrives before the subagent's notification, the stream stays open, and the orphaned shell task is reported `stopped` at session end. |
| `probe1-background-fanout.loop.ndjson`, `probe2-lead-ends-early.loop.ndjson` | GOLDENS, not recordings: the run loop's whole transcript over each probe — every run event, every watchdog call, every raw-log write, when input ended — captured from the loop before it stopped reading message shapes (ADR-0012 amendment) and pinned by `src/lib/run_loop.replay.test.ts`. A diff is a behaviour change; regenerate with `AEP_UPDATE_GOLDEN=1 pnpm test` only when that change is intended, and never edit one by hand. Re-recording a probe means regenerating its golden in the same change. |
| `opencode-s2b-foreground-fanout.jsonl` | **The OpenCode reference shape.** OpenCode 1.18.32 via `@opencode-ai/sdk` 1.18.32, claude-haiku-4-5, 322 bus events, recorded with `probes/opencode-probe.mts`. Three FOREGROUND `task` calls to the one `general` subagent in one turn (the experimental background flag unset — the platform's shape), one of which spawns a depth-2 child: every child's `session.created` carries `parentID`, the spawning part's `running` update carries `metadata.sessionId` and lands just after the child's `session.created`, reports are each child's last text part, the root idles once, last (13.7s, after every child), `todo.updated` ×2. Session summaries are zero throughout. |
| `opencode-s1c-guards-allowlists.jsonl` | Same versions, 371 events: a claude-sonnet-5 lead with the `general` subagent overridden to claude-haiku-4-5 (`s1c.overrides.json`), so the run has two models. The platform's guard plugin refuses a write outside the project (with its marker) and an http fetch to 127.0.0.1, the allowed skill loads and a skill outside the allowlist is never offered, the disabled `explore` agent is refused by the task permission, two models with cost on every assistant message, and a checkout's hostile `opencode.json` / `.opencode/plugin/` has no effect under `OPENCODE_DISABLE_PROJECT_CONFIG=1`. |
| `opencode-s2c-background-experimental.jsonl` | S2b's prompt with `OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=true`, 386 events, recorded past the close rule (`PROBE_PAST_CLOSE_MS`) — NOT the platform's shape, kept for the day the feature is adopted. Launch acks, the root idles at 6.7s and is woken by `<task state="completed">` user messages. Its replay shows the close rule firing before the lead's wrap-up turn, which is why the start-time assertion refuses the flag. |
| `opencode-s4i-instructions.jsonl` | One session, no fan-out, 77 events: `instructions: [file]` appends `s4i.instructions.md` (the OpenCode glossary with a marker at its END, where the glossary goes) and the lead quotes the marker. |
| `opencode-*.loop.ndjson` | GOLDENS for the four recordings: the loop's whole transcript through the OpenCode session stream, classifier and translator (`src/runtime/opencode/replay.test.ts`). Same rules as the probe goldens above. |
| `probes/opencode/` | Each recording's inputs (prompt, `s1c.overrides.json`, `s4i.instructions.md`), the config the probe wrote, and a summary derived from the recording (`*.summary.json`: per-session reports, per-model usage). |
| `run-2026-09-04-v1.ndjson` | A real 55-minute playground coding run, 759 lines of the v1 NDJSON feed the runner emitted. Two foreground fan-outs, 255 `tool_use`, 257 `tool_result`, one `result`. The v1 → v2 lift is tested against it. |

Each Claude `.jsonl` line is one SDK message exactly as `query()` yielded it.
Each OpenCode `.jsonl` line is one event exactly as `GET /event` delivered it,
plus a `t` the probe stamped (ms since start) that the replay uses as the
translator's clock. Paths inside them are the probe's temporary workspace on the
machine that recorded them — the OpenCode ones normalised to `/scratch/…`;
nothing in them is secret.

## Re-recording after an SDK bump

`probes/probe1.mts` and `probes/probe2.mts` produce the two recordings. Run them
from a directory that resolves `@anthropic-ai/claude-agent-sdk`, with a
signed-in Claude Code or an API key in the environment:

```
npx tsx probes/probe1.mts probe1-background-fanout.jsonl
npx tsx probes/probe2.mts probe2-lead-ends-early.jsonl
```

Both pin `claude-haiku-4-5` to keep a run near $0.20. If a re-recording changes
what the translator sees, the answer is a translator change plus an ADR
amendment, not an edited fixture.

## Re-recording the OpenCode fixtures

`probes/opencode-probe.mts` records one session under the runner's OWN config
builder, launcher, event queue and close rule (so a re-recording is made the way
a run is started), with the guard plugin built fresh; `probes/opencode-probe-config.mts`
is the model-free check — it boots a server exactly as a run does
(`bootOpencode`), runs the four start-time assertions and prints what the
server reports, with no model call. Both need `opencode` on `PATH`
(`npm install -g opencode-ai@1.18.32`, or run them inside `aep-runner-opencode:dev`):

```
npx tsx test/fixtures/probes/opencode-probe-config.mts <projectDir> <pluginDir>   # free
npx tsx test/fixtures/probes/opencode-probe.mts out.jsonl <projectDir> probes/opencode/s2.prompt   # real model calls
```

The recording probe makes real model calls (ANTHROPIC_API_KEY; 1–5 cents each
on claude-haiku-4-5, about 8 cents for S1c's Sonnet lead). It gives the server a
scratch HOME holding only `~/.cache/opencode` and `~/.config/opencode`, as the
image's pre-warmed home does, so the developer's own skills and shell startup
files stay out of the recording. Each workspace is a fresh `git init` with one
commit, and the paths are normalised to `/scratch/spike-proj` afterwards:

```
opencode-probe.mts opencode-s2b-foreground-fanout.jsonl <ws> probes/opencode/s2.prompt
OPENCODE_EXPERIMENTAL_BACKGROUND_SUBAGENTS=true PROBE_PAST_CLOSE_MS=15000 \
  opencode-probe.mts opencode-s2c-background-experimental.jsonl <ws> probes/opencode/s2.prompt
PROBE_INSTRUCTIONS=probes/opencode/s4i.instructions.md \
  opencode-probe.mts opencode-s4i-instructions.jsonl <ws> probes/opencode/s4i.prompt
PROBE_MODEL=claude-sonnet-5 PROBE_SECRET=STAGED-SECRET-XYZ \
  opencode-probe.mts opencode-s1c-guards-allowlists.jsonl <ws> probes/opencode/s1c.prompt probes/opencode/s1c.overrides.json
```

S1c's workspace also holds `.claude/skills/allowed-skill/` (greets with
`GREETING-FROM-ALLOWED-SKILL`), `.agents/skills/forbidden-skill/` (a farewell
skill outside the allowlist), and a hostile `opencode.json` (instructions naming
`pomegranate-47`) and `.opencode/plugin/hostile.js` (pushes `HOSTILE PLUGIN
LOADED` into the system prompt). A re-recording that changes what the translator sees is a translator
change, a regenerated golden and an ADR-0015 amendment — never an edited fixture.
