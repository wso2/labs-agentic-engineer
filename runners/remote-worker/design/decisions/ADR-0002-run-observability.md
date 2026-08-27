# ADR-0002 — What a coding run records about itself

**Status:** accepted

## Context

A run deleted three generated files and no artifact said why. Diagnosing it took
a from-scratch reproduction in the runner image, because neither the progress
feed nor the SDK transcript could answer the question.

Measuring one real fan-out run (2 subagents, 118 tool calls) showed why. Counting
content blocks by stream:

| stream | thinking | text | tool_use |
|---|---|---|---|
| main (orchestrator) | 16 | 10 | 41 |
| subagent — todo-api | **0** | **0** | 49 |
| subagent — todo-webapp | **0** | **0** | 28 |

**The SDK forwards only `tool_use` blocks from a subagent.** Reasoning and
narration are stripped. So 77 of 118 tool calls — all the implementation — ran
in streams where the only observable act is a tool call.

A second run, on a rebuilt image, showed the surface had **changed underneath
us**: `parent_tool_use_id` was null on every message, and no subagent content
was forwarded at all. Subagent work now arrives as `system` messages —
`task_started` / `task_progress` / `task_notification`, keyed by `task_id`. The
2 subagents' 48 steps were invisible: the feed showed 82 lines for a run that
created 27 files and passed two builds. Anything reading `parent_tool_use_id`
alone is now silently blind.

Four further gaps, all verified rather than assumed:

- `emitter` was recorded on all 77 subagent lines but the playground renderer
  dropped it, and even the console could not say *which* subagent: the
  translator read `parent_tool_use_id` for its truthiness and discarded the id.
- Tool outcomes were never translated at all. The run's **4 `is_error` results**
  — including the `bal build` that failed after 2m52s — rendered as successes.
- Nothing measured a call, so a real **8m49s** stall inside `bal tool pull` was
  indistinguishable from a dead run.
- `progress.ndjson` was advertised as NDJSON and was not: `console.log` wrote
  bare text onto the same file descriptor.

## Decisions

1. **The feed carries subagent identity, not just subagent-ness.** `emitterId`
   is the subagent's stable id; `emitterLabel` is the description the main agent
   gave it. Recording the label requires the translator to remember the fan-out
   call, so it is a per-run factory (`createSdkTranslator`) rather than a free
   function — state per run, never module scope.

2. **Both subagent surfaces are translated, and they share ONE identity.** The
   `parent_tool_use_id` path and the `task_*` path coexist in the translator.
   The cluster and the playground can be running different images, and the
   failure mode of guessing wrong is not an error — it is a run whose entire
   subagent half silently vanishes. The two are distinguished by data, never by
   a version check.

   A third run then showed both surfaces live *at once*, narrating every step
   twice — `[#1] Running Pull the OpenAPI tool` beside `[#2] bal tool pull
   openapi`, one subagent read as two. The spawning tool-call id is therefore
   the canonical identity: `task_started.tool_use_id` **is** the
   `parent_tool_use_id` of the forwarded copies, so both channels resolve to one
   subagent instead of each inventing one.

   Which channel becomes a ROW was then settled by measurement, against an
   earlier belief recorded here. The forwarded copies do **not** lag in a batch
   until the subagent finishes: after one catch-up burst of roughly nine steps,
   the gaps fall to 19.9s, 2.6s, 5.3s. Both channels cover the same window. So
   the forwarded `tool_use` is the row — it carries the actual command, and it is
   the only thing a `tool_result` can pair with — and `task_progress` becomes an
   `activity` event instead (decision 9).

3. **`task_type` decides attribution, and it is not cosmetic.** The same
   `task_*` messages carry two different things: `local_agent` is a fanned-out
   subagent whose lines belong to IT, and `local_bash` is the main agent's own
   backgrounded command. Treating them alike would file `npm install` under work
   the agent delegated.

4. **A task settles on its own `tool_result`, and `task_notification` is
   dropped.** This is the correction of a belief that cost a whole class of
   detail. The tool_result for a task was read as a launch acknowledgement and
   suppressed; measured, it is not. The `Agent` call is message 44 and its result
   is message **168**, carrying `status`, `totalDurationMs`,
   `totalToolUseCount` and `toolStats`. For a backgrounded command the result
   carries `Exit code 1` and the compiler's own ERROR lines, and its
   `parent_tool_use_id` names the subagent that ran it. `task_notification`
   arrives one message earlier carrying none of that — only the description the
   task was launched with — so emitting it would settle every task twice, the
   first time with the poorer report. What `task_started` still does is start the
   clock, which is what makes a duration real when the SDK reports none.

   **One exception, added after a run took the other path (decision 13): a fan-out
   launched with `run_in_background`.** There the tool_result really *is* a launch
   acknowledgement — `{isAsync: true, status: "async_launched"}` at +2ms, no
   totals — and the notification, arriving six minutes later with
   `usage.duration_ms` and `usage.tool_uses`, is the only completion signal that
   will ever come. So the rule is not "the notification is worthless", it is "the
   notification is the settle exactly when the result was not one". A foreground
   fan-out emits no agent notification at all, verified against a recorded run, so
   reading it cannot double-settle one. The launch itself produces no line:
   reported as an outcome it read `async_launched · 0.0s` and, because the status
   is not `completed`, painted a section that went on to succeed as a failure.

13. **Fan-out runs in the foreground; the runner enforces it.** `run_in_background`
    is not what makes subagents concurrent — several fan-out calls in ONE assistant
    turn is, and the SDK dispatches those together. Measured across two runs of the
    same work: foreground issued both subagents inside one API response 3s apart
    and produced 161 attributed events; backgrounded issued them in separate turns
    **124s apart** and produced **zero** — `parent_tool_use_id` was null on all 252
    messages, so 33 and 47 tool calls reached the feed as nothing but narration.
    Backgrounding therefore costs the entire implementation phase of the feed and
    does not buy the parallelism it looks like it buys. A `PreToolUse` hook
    (`lib/fanout_foreground.ts`) rewrites the flag rather than denying the call —
    denying would cost the fan-out, and only the detachment is the problem — and
    announces each rewrite on the feed, because silently altering what the model
    asked for is expensive to rediscover. The `aep` skill says the same in prose;
    prose is advice and the hook is the guarantee, and decision 4's exception is
    what keeps the feed honest if a mismatched image ever bypasses it.

    **Corrected after the SDK 0.2 → 0.3 bump: the hook must act on the flag's
    ABSENCE.** It originally rewrote only `run_in_background: true`, which was
    right while an omitted flag meant foreground. SDK 0.3.220 reverses that
    default — `AgentInput.run_in_background` reads "Agents run in the background
    by default … Set to false to run this agent synchronously" — and the model
    omits the flag, so the guard stopped firing in exactly the case it was written
    for. The cost was worse than a dark feed: the first milestone run on 0.3.220
    detached both subagents and then **ended while they were still working**,
    emitting `result: success` after 159s with one component left as a `bal
    openapi` stub and the other never created. It also reached for
    `ScheduleWakeup` trying to wait for them, which is why that tool is now in
    `DISALLOWED_TOOLS` (`runner.ts`). Probed directly against 0.3.220: strip the
    flag → `async_launched`; force `false` → the subagent's result returns inline.
    The predicate is therefore "not explicitly `false`", and the regression pin is
    the omitted-flag case in `fanout_foreground.test.ts`. Two lessons worth more
    than the fix: a default this load-bearing must be asserted, not assumed, and
    `allowedTools` was never enforcing anything under `bypassPermissions` — it
    still named `Task`, a tool 0.3.220 does not define, and nothing failed.

5. **Tool outcomes are events.** `tool_result` carries `ok`, `durationMs` and,
   on failure only, the error text. Success output is bulky, uninteresting, and
   the likelier place for a secret. `ok` is a POINTER in the Go mirror: a plain
   `bool` with `omitempty` would drop `false` on the wire and render a failed
   call as a success — the exact defect the event exists to fix. `exitCode` is a
   pointer for the same reason: absence must read as "no code was reported", not
   as "exited 0".

   A failure reports ONE line, chosen rather than taken: the exit code parsed off
   the SDK's own `Exit code N` prefix, then the first line that announces a fault.
   A real `bal build` prints nine lines of dependency pulls before its first
   `ERROR`, so "the line after the code" would diagnose a failed build as
   "Compiling source". A line ending in a colon takes the line it was
   introducing, because on its own it is a heading.

   **A failed FAN-OUT is the exception, and it prints everything it has.** The
   one-line rule holds because the command is on the row above and the whole
   output is in `claude.log`. A subagent has neither: its transcript is not on
   this feed, and `claude.log` dies with the pod. Measured live, that left a
   22-minute subagent arriving as a bare `ok:false` with the reason recorded
   nowhere. So a failed fan-out emits a SECOND event — an `error` log line
   attributed to the subagent, carrying the tool result's text bounded to 10
   lines / 2,000 characters. Second event rather than a richer row, because
   `summary` on that row is contractually the subagent's LABEL, which the console
   renders as the section heading. It is emitted even when the result carried no
   text: "the SDK gave no reason" is itself the finding, and is otherwise a whole
   run spent re-establishing it.

6. **Reasoning capture was rejected, on evidence.** The obvious fix — translate
   `thinking` blocks — cannot reach the subagents, which is where the incident
   happened and where 65% of the work happens. Instead the `aep` skill requires
   a one-line `echo` naming the reason before discarding existing work. Tool
   calls are the only channel that survives the forwarding boundary, so the
   reason has to *be* a tool call to be recorded at all. **Superseded in part by
   decision 16** — the forwarding boundary was the SDK's default, not a fixed
   property of it. The `echo` rule stays: it is what puts a reason on the *feed*,
   which is a different audience from the transcript.

7. **Silence reports what it is waiting on.** The watchdog distinguishes the
   faults a silent feed can mean — a tool in flight (that call is slow or stuck,
   and the line names it), a subagent running (its model turn is the slow half,
   and the line names which subagent), or neither (the lead's own turn). It warns
   and repeats; it never fails a run on its own clock, because a long dependency
   pull is legitimate. A SIGTERM handler dumps the same snapshot, so a killed
   run still explains itself.

   **The subagent case is read off `emitterId`, not off the fan-out call.**
   Decision 11's sibling: the translator gives an `Agent` call no `tool_use`
   event of its own, so the watchdog — which sees only emitted events — never had
   the call in flight and could not name it. It claimed otherwise for as long as
   the claim existed, in this ADR, in the module header, and in a test that
   asserted against a hand-made `{tool: "Agent"}` event production never emits.
   Measured live: a fan-out went silent for ten minutes and every report said
   "no tool in flight — waiting on the model", pointing at the lead while a
   22-minute `Agent` call was the thing being waited on. The only trace of a
   running subagent on this stream is the attribution stamped on the lines it
   produces, so that is what the watchdog registers. Its clock therefore starts
   at the subagent's FIRST line rather than at the call — a few seconds late, and
   the only start this stream carries. Several at once are counted, not guessed
   between: the lines interleave and any of them could be the silent one.

14. **A stalled model turn names its cause, and API retries are reported on
    every run.** "Nothing in flight" was where decision 7 stopped, and it is a
    symptom with two very different causes. The SDK already distinguishes them
    and we were discarding the evidence: it emits `system`/`api_retry` per
    retryable failure (`attempt`, `max_retries`, `retry_delay_ms`,
    `error_status`, `error`) and `from-sdk.ts` dropped it with every other
    unrecognised system subtype. Measured against a dead endpoint: 8 retries in
    69s and not one line about any of them. So retries now become a `warn` log
    event and the watchdog's line names them — `(API retry 7/10, overloaded,
    last 10s ago)` — in every branch, because a stall inside a subagent surfaces
    under that subagent's line and that is where the cause is hardest to guess
    from outside. Whether an `api_retry` raised INSIDE a subagent reaches the
    lead's stream at all is **not established**: none arrived during the live
    stall above, which is equally consistent with "no retries" and "not
    forwarded". An empty cause therefore claims nothing.

    This is **not** gated behind a debug flag, and that is deliberate on three
    counts: a healthy run emits nothing (no retries, no messages, no lines);
    `error` is a closed enum, so unlike stderr or the debug log there is no free
    text here to carry a prompt or a credential into a build log the console
    forwards; and overload is load-dependent, so a flag would be off during
    every incident worth having it for.

    A retry is recorded but does **not** count as activity. Routing it through
    `observe()` would reset the idle clock, and the measured backoff climbs
    0.2s → 33.6s — all inside the 120s window — so the watchdog would have gone
    quiet through exactly the stall it exists to report. Verified live: the
    report now fires mid-storm naming attempt 6/10.

15. **The developer options are opt-in, and they are files.** `debugFile`,
    `stderr` and `includePartialMessages` are on for every playground run and
    off in a pod unless `AEP_RUNNER_DEBUG=1` opts one in. The split is by sink,
    not by taste: nothing collects a pod's files (`claude.log` has been written
    unconditionally for as long as it has existed and only the playground has
    ever read one), so these are for someone sitting in front of a run
    directory — and the debug log holds prompt text, which is why it stays off a
    channel the console renders to a browser. Streaming frames reach neither the
    feed nor `claude.log`; they exist so the watchdog can tell a long generation
    from a wedged one, and writing one JSON line per token would turn a
    diagnostic into the hang it reports on.

    They do not change *when* the watchdog fires — frames are recorded without
    counting as activity, same as retries — because a diagnostic that alters the
    symptom cannot be used to reproduce it.

    `stderr` was where we first went looking for retry detail and it carries
    none: probed against the same dead endpoint it produced one unrelated
    startup warning while all 8 retries went past on the message channel. It is
    kept as a sink for what else the CLI says, not as the diagnosis.

16. **The reasoning IS capturable now, and it joins the developer options.**
    Decision 6 rejected reasoning capture because the forwarding boundary put
    the subagents out of reach. That boundary is the SDK's *default*, not a
    property of it: `forwardSubagentText` forwards a subagent's text and
    thinking as messages carrying `parent_tool_use_id`, and it was never set.

    It takes two options, because either alone leaves the transcript unable to
    answer the question. Measured on the 2026-08-14 playground run, with
    neither set: 7 thinking blocks in the lead session, **every one of them
    `thinking: ""`** — signed, empty, and accompanied by 62
    `system`/`thinking_tokens` events counting reasoning whose text nothing
    kept; and 120 subagent tool calls with zero blocks of any other kind. So
    `thinking: {type: "adaptive", display: "summarized"}` is what makes a block
    carry words, and `forwardSubagentText` is what makes the blocks exist for
    the streams that do the work.

    They sit in `debugQueryOptions` with the other three, for the reason
    decision 15 gives: volume, and a sink nothing collects. A pod's transcript
    would grow by every subagent's prose to no reader.

    The progress feed is unaffected, and that is checked rather than hoped:
    `assistantToolUseBlocks` selects `type === "tool_use"` and ignores every
    other block kind, so the new content reaches `claude.log` and stops there.
    Decision 12 still holds — this is developer detail that is READ, not fed.

17. **The system messages that explain a silence are read, not dropped.**
    Decision 14 took `api_retry` off the discard pile; four more subtypes were
    still on it, and each is a stall or a death the feed reported as silence.
    `compact_boundary` is the one that matters most and the only benign entry:
    an auto-compaction is minutes of total quiet with no tool in flight and no
    retry — indistinguishable from a wedge, and now a line
    (`[compact] auto compaction 152k → 38k tokens in 47s`).
    `model_refusal_no_fallback` / `model_refusal_fallback` say a turn ended
    because the model refused, `permission_denied` says a call was denied rather
    than answered (on the stream only since SDK 0.3.223 — before it a
    `DISALLOWED_TOOLS` denial reached the feed as a puzzling tool result), and
    `worker_shutting_down` says the worker left. Ungated for the same three
    reasons as decision 14: a healthy run emits none of them, every field
    printed is a closed enum, a number, an id or ONE prose field bounded to 200
    characters, and none of these faults is one a flag would be on for.
    Deliberately not fed to `watchdog.observe` — none is the agent making
    progress, and an idle report that fires slightly early is the safe direction.

8. **`console.*` is converted, not merely scrubbed.** It shares the fd with the
   feed, so a bare line makes the stream unparseable — and a watchdog cannot
   watch a feed it cannot parse. Every call becomes a typed `log` event. The
   BFF's raw-line fallback stays as a safety net for output that never went
   through `console` at all.

9. **The intent phrase has exactly one home: a collapsed section's status.** A
   `task_progress` becomes an `activity` event, which every renderer formats to
   no text. Inline beside the command it is status text, not progress — "Running
   List project root contents" next to `ls` earns nothing — and the user's own
   verdict on seeing it was that a status update inside a progress list "kinda
   doesn't make sense". Where it does earn its place is the one line describing
   work the reader has chosen not to expand.

10. **A subagent section reports the SDK's figures, not ours.** `status`,
    `durationMs`, `toolCount`, `linesAdded` and `linesRemoved` come off the
    fan-out call's result; `totalDurationMs` of 209158 matched a hand-measured
    3m29s exactly. `+553/−4 lines` cannot be derived from this feed at all, since
    a subagent's per-edit line counts never appear in its own events. Collapsed,
    that line is everything a reader gets about the subagent, so it carries the
    verdict and the figures rather than a line count.

11. **Merging an outcome onto its action is a RENDERER capability, not a wire
    shape.** An action is emitted the instant the SDK yields it, before the
    command runs — that is what makes the feed live. Its outcome is not knowable
    for another 25 seconds on a cold build. Delaying the action line to align
    them was refused: it buys one tidy row per step at the price of 25 seconds of
    silence per build. So the wire keeps two events, the console (which holds
    every line in state) attaches the outcome to the action's row, and a terminal
    prints it as a continuation and re-renders the whole run merged at the end.
    The wording is shared either way (`@aep/progress-view`), so the fast local
    loop is a faithful preview of the console rather than a second dialect.

12. **Developer detail is READ, never fed.** What took the time, exactly what
    failed, and the reasoning that led there are a different audience's
    questions, and answering them in the progress feed buries the two lines that
    matter. `claude.log` already holds every message; `progress.ndjson` holds the
    durations the SDK does not stamp. `play <dir> log [--slow|--thinking]` joins
    them on demand. A derived index was rejected: it would be a cache of an
    analysis, and one that can drift from the truth is worse than none.

## Consequences

- Roughly 2× the events on the feed (118 → 238 on the measured run). Renderers
  drop deliberately-silent lines — a fast, successful `tool_result` formats to
  the empty string — so the rendered count grows far less.
- The translator and the watchdog are per-run objects. Two runs sharing one
  would mislabel lines, which is worse than losing the detail.
- `emitter`, `emitterId`, `emitterLabel` and `ok` are all absent-means-something
  fields. Absence of `emitter` is "main"; absence of `ok` is "not a tool
  result", never "succeeded".
- **Known limit: a subagent's FIRST batch of steps has no usable duration.** The
  forwarded channel opens with one catch-up burst — measured on a live run, 13
  steps arriving inside the same millisecond — so pairing a call with its outcome
  there times the batch, not the work. Everything after streams in real time and
  measures correctly (a `bal build` reported 11.6s, and a 13s gap between a call
  and its result came through as 13s). The 3s floor is what keeps this honest:
  a burst-timed call reports 0-1ms, which renders as no duration at all rather
  than as a fast one. The alternative — pairing each step to a
  `task_progress.usage.duration_ms` delta by index — was not built: it would
  attach a *wrong* duration to a command whenever the index slipped, which is
  worse than the current omission.
- **Known limit: a section settled from a notification has no line deltas.**
  `toolStats` rides the tool_result a backgrounded fan-out never produces, so such
  a section reports its duration and step count and omits `+N/−N lines` rather
  than claiming zero. Only reachable when the foreground hook is bypassed.
- **Known limit: a `local_bash` task's own `task_*` messages carry no owner.**
  `task_started` has no owning task id, so nothing on that channel says which
  subagent backgrounded the command. This is survivable only because those
  messages no longer produce lines: the command's row and its outcome both come
  from the forwarded channel, which does carry `parent_tool_use_id`. Correlating
  the `task_*` half by "whichever subagent was active" would be a guess, and a
  feed that guesses attribution is worse than one that declines to.
