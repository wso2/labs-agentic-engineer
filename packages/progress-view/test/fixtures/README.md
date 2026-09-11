# Run-event fixtures

Three **real** sessions in the v2 `RunEvent` envelope, for the crew model to be
tested against. They are frozen artifacts: regenerate them only when the
recording they came from changes, never by hand.

Hand-written fixtures agree with whatever the code under test does. These do
not — every gap, every out-of-order settle and every unhelpful phrase in them is
something a run actually produced.

| File | Events | What it holds |
|---|---|---|
| `probe1-background-fanout.v2.ndjson` | 40 | A 17-second probe: three subagents launched **backgrounded** in one turn, one of which fans out again to a depth-2 child. Every agent settles with its own report. Carries `agent_progress`, a `notice`, and a `heartbeat`. |
| `run-2026-09-08-phantom-agents.v2.ndjson` | 151 | A slice (seq 60–210, 5m50s) of a live coding run **while its producer was broken**: the adapter minted an agent id from the `tool_use_id` of every blocking wait, so two of the five authors on this feed are agent ids that no `agent_started` ever declared. Held because a consumer cannot tell a phantom from a legitimately undeclared agent, and this is what it must do with one. No settles of any kind. |
| `run-2026-09-04.v2.ndjson` | 761 | A real 55-minute coding run (09:25:39 → 10:19:57). Two subagents in sequence — 41m11s / 162 tools / +2069−78, then 7m50s / 74 tools / +69−37 — so the lead's lane has two `waiting` stretches. Holds real 60s+ silences with a tool call still unanswered, which is what the amber rule is tested against. |

The first two were not v2 when they were made, and each was converted **once**,
by a producer that converts for real — this package must not grow a translator
of its own, and it depends on neither the runner nor aep-api. The third needed no
conversion: it was recorded from a pod that already spoke v2.

## How they were generated

### `probe1-background-fanout.v2.ndjson`

Source: `runners/remote-worker/test/fixtures/probe1-background-fanout.jsonl`
(Claude Agent SDK 0.3.247, 92 messages).

Produced by the **runner's own pipeline** — `consumeRun` driving
`createClaudeAdapter({ taskKind: "implementation" })` and `createRunWatchdog`,
exactly as `runners/remote-worker/src/lib/run_loop.test.ts` replays it. A
throwaway `tsx` script collected `emit` calls and stamped each event the way
`emitter.ts` does: `v: 2`, a monotonic `seq` from 1, and `agentId: "lead"` where
the adapter named no agent.

`ts` is the **recording's own clock**: 47 of the 92 SDK messages carry
`timestamp`, and each emitted event takes the last one seen. The run's first two
events precede the first timestamped message, so they take the run's first known
timestamp rather than an empty one.

### `run-2026-09-04.v2.ndjson`

Source: `services/aep-api/internal/delivery/codingagent/testdata/run-2026-09-04-v1.ndjson`
(759 lines of the v1 `RunProgressLine` envelope, recorded 2026-09-04 by the
runner image that still spoke v1).

Produced by **aep-api's lift** (`run_event_lift.go`), which is the reader every
pre-cutover cycle is replayed through. The lift is package-private, so a
throwaway `_test.go` generator was added to a copy of the package outside the
repo, ran `newLifter().line(…)` over the recording and marshalled each
`gen.RunEvent`. 759 lines in, 761 events out — the two extra are the
`agent_started` announcements the lift infers, because a v1 runner declared none.
That is why both subagents here carry `role: "inferred"`.

Generated 2026-09-07 against the lift as it stood in phase 2. The lift's job is
to be faithful to the recording rather than flattering: it renders a v1 `phase`
line as an `agent_progress` whose phrase is the raw phase id
(`workspace_provisioning`), and the crew model shows exactly that. A phrase is
the runtime's own words, and this package does not improve them.

### `run-2026-09-08-phantom-agents.v2.ndjson`

Source: the runner's own recorder, captured live from the coding cycle of
2026-09-08 (`cycle1.recorder.ndjson`, seq 60–210). **Not converted at all** —
this is the v2 feed exactly as the pod emitted it, which is the point: it is
evidence of a producer defect, so a translation step would be a chance to
launder it.

The defect is fixed at the producer (`claude_adapter.ts`'s `authorOf`, and the
invariant test beside it), so no run emits this shape any more. The fixture stays
because the CONSUMER's behaviour under it is a decision worth pinning: an author
the producer never declared is counted and shown, never dropped. Dropping it
would need this package to recognise one runtime's id prefix — which the contract
forbids, and which would in any case delete both real agents of
`run-2026-09-04.v2.ndjson`, whose ids are `toolu_…` because the v1 lift keys an
inferred agent by the call that spawned it.

Scanned for credentials and personal data before it was committed; it carries a
demo expense-tracker project's file paths and shell commands and nothing else.
