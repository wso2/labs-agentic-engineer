# Runner test fixtures

Recorded inputs for the progress translator and the runner loop. They are the
evidence the run-events design was measured on, kept here so a test can replay a
real session instead of a hand-written one, and so a future SDK bump can be
re-measured rather than assumed.

| File | What it is |
|---|---|
| `probe1-background-fanout.jsonl` | Claude Agent SDK 0.3.247, 92 messages. Three subagents launched with `run_in_background: true` in one turn; one of them spawns a depth-2 child. Shows `task_started` with `is_backgrounded` and `spawn_depth`, assistant messages attributed through `parent_tool_use_id`, and `task_notification` carrying `summary` / `output_file` / `usage`. |
| `probe2-lead-ends-early.jsonl` | Same SDK, 40 messages. The lead ends its turn while a background subagent is still running. The first `result` arrives before the subagent's notification, the stream stays open, and the orphaned shell task is reported `stopped` at session end. |
| `run-2026-09-04-v1.ndjson` | A real 55-minute playground coding run, 759 lines of the v1 NDJSON feed the runner emitted. Two foreground fan-outs, 255 `tool_use`, 257 `tool_result`, one `result`. The v1 → v2 lift is tested against it. |

Each `.jsonl` line is one SDK message exactly as `query()` yielded it. Paths
inside them are the probe's temporary workspace on the machine that recorded
them; nothing in them is secret.

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
