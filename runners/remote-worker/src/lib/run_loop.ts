/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Reads one session's SDK message stream to its end and decides when the RUN is
// over.
//
// It used to live inside `startCodingRun`, which built the query and consumed it
// in one function — so the only way to exercise the rule below was to start a
// real session. It is its own module now because the rule is the whole reason
// this code exists, and a rule nothing can replay is a rule nobody can check.
//
// **The run settles when the event source closes, not on the first `result`.**
// A `result` message is one TURN ending. Until SDK 0.3.247 those were the same
// thing here, because a fan-out was always foreground and the lead could not end
// a turn with work still outstanding. It can now: measured on
// `test/fixtures/probe2-lead-ends-early.jsonl`, the lead launches one background
// subagent and ends its turn at message 16 of 40 — the subagent's own attributed
// steps arrive at messages 18-25, its completion notification at 28, a SECOND
// `result` at 37, and the last message of all is the orphaned shell task being
// reported `stopped`. Returning on the first `result` killed the pod with the
// subagent mid-flight and 24 of 40 messages unread, which is a whole
// component's work lost with a green run to show for it.
//
// So the loop keeps reading, records the LATEST result's outcome, and settles
// when the iterator closes. The exit code follows the settle: it is 0 only if
// the final outcome was a success.
//
// **`run_settled` is the RUNNER's event, and there is exactly one.** The adapter
// turns every `result` message into a `turn_ended`, which goes on the feed as
// it happens and is informational: a turn ended, the run did not. This loop
// remembers the newest one and writes `run_settled` when the source closes,
// carrying that turn's outcome and its usage. That separation is what v1 could
// not express — it had one terminal kind, so the loop had to hold the turn back
// and several would have settled one run several times, the first (on probe 2)
// with a verdict the run went on to disprove.
//
// **Usage is not summable.** The runtime reports its token counts cumulatively
// across turns (see `usageFromResult` in the claude adapter), so the last
// `turn_ended`'s usage IS the run's total, which is what makes carrying it
// forward onto `run_settled` correct rather than convenient.
//
// **A run that ends EARLY settles exactly once too, and this loop is still what
// writes it.** `RunTerminator` is the seam anything else ends a run through: the
// deadline guard and a fatal MCP auth failure both hand it a REASON, the one
// race below sees it, stops whatever is still running and writes the single
// `run_settled`. The seam exists because the obvious alternative shipped — the
// MCP policy's `onFatal` (`runner.ts`) emitted a settle of its own while this
// loop was still reading, so one run's feed could carry two of them, and every
// consumer treats a settle as terminal (`buildCrew` in `@aep/progress-view`
// settles every agent it never heard close on one). A settle followed by more
// of the run is a feed nobody can read back.
//
// **Input ends when the run is done, not when the first turn does.** The SDK
// closes the CLI's stdin the moment the FIRST `result` arrives for a plain
// string prompt, and stdin is also the channel every SDK-side hook answers on.
// Measured on 2026-09-07 (`track-each-hire3141-b`): the lead fanned out in the
// background, its turn ended, stdin closed, and the subagent's first Edit met a
// cancelled workspace-guard hook — which the CLI reports to the agent as "the
// user doesn't want to take this action", so both agents stopped with a green
// result and seven files written. The runtime therefore feeds the prompt as a
// stream it holds open, and THIS loop says when to let it go, through
// `RunStream.endInput`: at a `result` with no task still live, or — when the
// last live task settles after a `result` and nothing wakes the lead within a
// grace period — then. Ending it is what lets the CLI exit and the source close.
//
// **Anything that proves the run is still alive disarms that grace**, and the
// hole left when it did not is the same failure a second time. Streaming the
// prompt fixed the 2026-09-07 case; on 2026-09-19 (`simplest-crud-blog`) the
// lead's last subagent settled at 07:40:30 and armed the grace, the lead was
// generating throughout — `heartbeat waitingOn:model` at 07:40:30 and 07:40:40 —
// but liveness frames were `continue`d before they could reach the disarm, the
// grace expired at ~07:40:45, and the lead's next Edit at 07:40:55 met the same
// cancelled hook. It abandoned two fully-built components and settled green.
// Unhooked tools kept working right through it (two Reads succeeded at 07:41:10,
// an Edit failed at 07:41:13), which is what makes this look like a model that
// changed its mind rather than a channel that was closed under it. So the grace
// is armed only by task bookkeeping and is disarmed by ANY message that says
// this run is still working — including the liveness frames the watchdog
// deliberately ignores.
//
// **A run that never ends is not a settle**, which is what the deadline guard
// below is for: a pod is killed from outside when its Job's deadline passes, and
// a killed pod explains nothing. See `createRunDeadline`.
//
// **This loop reads no message shape.** Every rule above turns on WHAT KIND of
// message arrived — a turn ending, a task starting, a retry, a liveness frame —
// and the kind is the runtime's to say: `RunLoopOptions.classify` answers with a
// `MessageClass` (`runtime/port.ts`), and the loop branches on that. The words
// above say "a `result`" because the rules were measured on Claude Code; the
// code says `turn_end` and `task_bookkeeping`.

import type { ApiRetryInfo, MessageClassifier } from "../runtime/port.js";
import { checkPreload, preloadWarning } from "./skills_preload_check.js";
import { emit as defaultEmit, LEAD_AGENT_ID, type RunEventInput, type RunEventUsage } from "./progress/emitter.js";
import type { RunWatchdog } from "./progress/watchdog.js";
import { withTimeout } from "./with_timeout.js";

// The race's "this run is over" arm. A unique symbol key rather than a sentinel
// object so an early end can never be confused with an IteratorResult — which is
// a value the SDK controls and we do not.
const TERMINATED: unique symbol = Symbol("run terminated");

/** An early end, wrapped so it can travel through the race with its reason. */
type EarlyEnd = { readonly [TERMINATED]: RunTermination };

function earlyEnd(reason: RunTermination): EarlyEnd {
  return { [TERMINATED]: reason };
}

/**
 * Which arm of the race won.
 *
 * A type predicate rather than an equality check, so the compiler narrows the
 * other arm to an IteratorResult on its own — the earlier symbol comparison
 * needed an unreachable second branch to get there.
 */
function isEarlyEnd(step: IteratorResult<unknown> | EarlyEnd): step is EarlyEnd {
  return TERMINATED in step;
}

/** One turn ending, held so its verdict and usage can become the run's settle. */
type TurnEnded = RunEventInput & { kind: "turn_ended" };

/** One SDK message translated into the canonical events it produced. */
export type RunEventTranslator = (message: unknown) => RunEventInput[];

/** What the entrypoints exit with, and why. */
export interface RunResult {
  exitCode: number;
  error?: string;
}

/**
 * The session, as this loop needs it: messages to read, and a way to stop a
 * task that is still running when time is up.
 *
 * Narrower than the SDK's `Query` on purpose — everything else `Query` offers
 * (interrupt, setModel, MCP surgery) is the caller's business, and a seam that
 * accepted the whole thing could only be exercised by starting a real session.
 */
export interface RunStream {
  messages: AsyncIterable<unknown>;
  /**
   * `Query.stopTask` — "Stop a running task. A task_notification with status
   * 'stopped' will be emitted."
   */
  stopTask(taskId: string): Promise<void>;
  /**
   * Let the CLI's input close. The runtime keeps the prompt stream open so the
   * SDK's hook channel survives the lead's first turn (see the header); this is
   * the loop telling it the run is over. Idempotent, and optional so a stream
   * that has no input to end (a recording) needs no stub.
   */
  endInput?(): void;
}

/**
 * How long, after the last live task settles following a `result`, the loop
 * waits for the lead to be woken for another turn before ending input anyway.
 * Measured: a completion wakes the lead within a second or two; the margin is
 * for a slow model, not a design allowance. A run whose lead is never woken
 * would otherwise hold the CLI open until the Job deadline.
 */
export const INPUT_GRACE_MS = 15_000;

/**
 * The clock that bounds a run, so the runner ends it rather than being killed
 * mid-sentence.
 *
 * A one-shot pod has a Job deadline; when it passes, the pod goes away with
 * whatever it was doing unrecorded — no result line, no watchdog snapshot, and
 * for a run with background subagents no way to tell "still working" from
 * "wedged". The guard fires BEFORE that, which is what buys the run the moment
 * it needs to stop its tasks and say so on the feed.
 *
 * `expiry` never rejects, so a race against it can only ever mean "time is up".
 */
export interface RunDeadline {
  readonly expiry: Promise<void>;
  /**
   * The whole run budget this guard defends, for the line it produces. Not the
   * same number as the delay: the guard fires a margin early, and the margin is
   * an implementation detail nobody reading the feed asked about.
   */
  readonly budgetMs: number;
  cancel(): void;
}

/**
 * The Job deadline, in seconds, as `activeDeadlineSeconds` states it.
 *
 * Absent means no guard, which is what keeps every existing caller behaving
 * exactly as it did: neither the dispatcher nor the playground sets this yet.
 */
export const RUN_DEADLINE_ENV = "AEP_RUN_DEADLINE_SECONDS";

/**
 * How long before the Job's own deadline the guard fires.
 *
 * It has to cover stopping the live tasks, the settle, and stdout draining
 * through a pipe the BFF is reading. A minute is generous for that and cheap
 * against the hour-scale budgets a coding run is given.
 */
export const DEADLINE_MARGIN_MS = 60_000;

// A stop that never answers would turn an early ending into the hang it exists
// to prevent, so the whole stop phase is bounded — for a fatal exactly as for a
// deadline, since both go through the same termination path. The run is ending either way;
// what matters is that the settle reaches the feed before the pod does not.
const STOP_TASKS_TIMEOUT_MS = 5_000;

/**
 * A deadline that fires `fireAfterMs` from now.
 *
 * `budgetMs` is what the feed line quotes — the run's whole budget, which is
 * larger than the delay whenever the guard is subtracting a margin or the run
 * had already spent time provisioning before this loop started.
 */
export function createRunDeadline(fireAfterMs: number, budgetMs: number = fireAfterMs): RunDeadline {
  let fire: () => void = () => {};
  const expiry = new Promise<void>((resolve) => {
    fire = resolve;
  });
  const timer = setTimeout(() => fire(), Math.max(0, fireAfterMs));
  // unref'd for the same reason the watchdog's timer is: a diagnostic must never
  // be the reason a finished run's process stays alive.
  timer.unref?.();
  return {
    expiry,
    budgetMs,
    cancel: () => clearTimeout(timer),
  };
}

/**
 * The deadline this process should run under, or undefined when none is set.
 *
 * Clocked from process start rather than from this call, because the budget
 * belongs to the POD: cloning the repo, mirroring the skills and installing the
 * git credential helper all happen before the first SDK message, and a guard
 * that ignored them would fire after the kill it exists to beat.
 *
 * The margin is capped at half the budget so a deliberately short deadline — a
 * developer probing the guard, a smoke run — still leaves a usable window
 * instead of firing the instant the loop starts.
 */
export function runDeadlineFromEnv(
  env: NodeJS.ProcessEnv = process.env,
  elapsedMs: number = process.uptime() * 1000,
): RunDeadline | undefined {
  const seconds = Number(env[RUN_DEADLINE_ENV]);
  if (!Number.isFinite(seconds) || seconds <= 0) return undefined;
  const budgetMs = seconds * 1000;
  const margin = Math.min(DEADLINE_MARGIN_MS, budgetMs / 2);
  return createRunDeadline(Math.max(0, budgetMs - margin - elapsedMs), budgetMs);
}

/**
 * Why a run is ending before its stream closed, in the words the feed will use.
 *
 * The reason travels WITH the termination rather than being derived at the end
 * of it, because there is now more than one thing that can end a run early and
 * a feed that said "terminated" without saying by what is the report a reader
 * has to go and reconstruct from the pod's logs — which outlive nothing.
 *
 * The three parts are the three places the reason has to appear:
 *   `source` — the bracketed origin every diagnostic line here carries
 *              (`[deadline]`, `[watchdog]`, `[workspace]`);
 *   `why`    — the sentence, in the feed's voice, minus anything only the loop
 *              knows (how many tasks it had to stop);
 *   `error`  — the settle's own `error`, which is also the process's exit
 *              reason. Kept separate because a settle is read on its own, out
 *              of the line's context.
 */
export interface RunTermination {
  readonly source: string;
  readonly why: string;
  readonly error: string;
}

/**
 * The seam anything outside this loop ends a run through.
 *
 * A promise rather than a callback, and that is the whole point: a callback
 * would have to DO the ending where it was called — which is how the MCP auth
 * policy came to emit a second `run_settled` while the loop was still reading.
 * Tripping this only states the reason; the loop, which owns the settle, does
 * the ending.
 *
 * Two consequences worth knowing before reshaping it:
 *
 *   - It is idempotent by construction. A promise resolves once, so a second
 *     fatal (or a fatal racing the deadline) cannot settle the run twice.
 *   - Tripping it after the loop has already returned is a no-op, which is the
 *     correct behaviour for a straggling failure — the MCP proxy can fail a
 *     last request while the session is closing, and a run that already ended
 *     green must not be rewritten as a failure by it.
 */
export interface RunTerminator {
  /** Resolves when someone ends the run early. Never rejects. */
  readonly requested: Promise<RunTermination>;
  /** End the run, with this reason. The first call wins; later ones are ignored. */
  terminate(reason: RunTermination): void;
}

export function createRunTerminator(): RunTerminator {
  let fire!: (reason: RunTermination) => void;
  const requested = new Promise<RunTermination>((resolve) => {
    fire = resolve;
  });
  return { requested, terminate: (reason) => fire(reason) };
}

export interface RunLoopOptions {
  /** This run's translator — see `RuntimeSession.translate`; never shared between runs. */
  translate: RunEventTranslator;
  /**
   * This run's classifier — see `RuntimeSession.classify`. Per run for the same
   * reason as the translator: it may remember what it has already said.
   */
  classify: MessageClassifier;
  /** This run's watchdog, fed the same events the feed gets. */
  watchdog: RunWatchdog;
  emit?: (event: RunEventInput) => void;
  /** Where every raw SDK message is kept (runtime.log). */
  record?: (message: unknown) => void;
  /**
   * The adapter's cumulative usage — see `RuntimeSession.usage`. Carried by a
   * settle no turn reported (an early end, a stream that closed or threw
   * without one); a normal settle carries the last turn's, which is the same
   * total.
   */
  usage?: () => RunEventUsage | undefined;
  /**
   * The skills the session was asked for, checked against the ones its `init`
   * says it resolved. Data rather than a callback: the mismatch is reported as
   * a feed line, and lines are this loop's business.
   */
  requestedSkills?: readonly string[];
  deadline?: RunDeadline;
  /**
   * The seam an early ender trips — see RunTerminator. Optional because a
   * replay has nobody to trip it, and because a run with neither this nor a
   * deadline is exactly the run this loop read before either existed.
   */
  terminator?: RunTerminator;
  /** Overrides INPUT_GRACE_MS; tests only. */
  inputGraceMs?: number;
}

/**
 * Read the stream to its end and return the run's outcome.
 *
 * Never rejects: a thrown stream is a failed run, and a caller that had to
 * handle both would have two ways to end one run.
 */
export async function consumeRun(stream: RunStream, opts: RunLoopOptions): Promise<RunResult> {
  const emit = opts.emit ?? defaultEmit;
  const record = opts.record ?? (() => {});
  const { translate, classify, watchdog, deadline } = opts;
  const usageSoFar = (): { usage?: RunEventUsage } => {
    const usage = opts.usage?.();
    return usage ? { usage } : {};
  };
  // The task ids this run knows are still running — what a termination has to
  // stop. Moved only by `task_bookkeeping`; which runtime words open and close
  // a task (and which deliberately do not) is the classifier's to know.
  const live = new Set<string>();

  // The newest turn's ending, kept so the settle can carry its verdict and its
  // usage. Undefined right up to the first `result` message, which is what
  // distinguishes "the run ended" from "the stream stopped".
  let lastTurn: TurnEnded | undefined;

  // Ending input, once. The grace timer is cleared on every exit path below,
  // which is what keeps a settled run from outliving it — deliberately NOT
  // unref'd: with the CLI gone nothing else holds the event loop, and an unref'd
  // timer then never fires at all.
  let inputEnded = false;
  let inputGrace: NodeJS.Timeout | undefined;
  const endInput = (): void => {
    if (inputEnded) return;
    inputEnded = true;
    clearTimeout(inputGrace);
    stream.endInput?.();
  };
  const disarmInputGrace = (): void => {
    clearTimeout(inputGrace);
    inputGrace = undefined;
  };
  const armInputGrace = (): void => {
    if (inputEnded || inputGrace) return;
    inputGrace = setTimeout(endInput, opts.inputGraceMs ?? INPUT_GRACE_MS);
  };

  const messages = stream.messages[Symbol.asyncIterator]();
  // Every way this run can end before its source does, as ONE promise: the
  // deadline expiring, and anyone who trips the terminator. Whichever arrives
  // first carries its own reason, so there is a single termination path and a
  // single settle however the run came to an end.
  //
  // Built once, not per message: every `.then` on a pending promise is retained
  // until that promise settles, and a long run reads thousands of messages.
  // Undefined when neither exists — and then there is no race at all, which is
  // the shape every replay test and every pre-deadline caller runs in.
  const ending = earliestEnding(deadline, opts.terminator);

  try {
    for (;;) {
      const step = ending ? await Promise.race([messages.next(), ending]) : await messages.next();
      if (isEarlyEnd(step)) {
        // The held turn's VERDICT is deliberately dropped. Whatever the last
        // turn reported, this run did not finish — it was ended with work
        // outstanding, and a success line here is the one thing nobody
        // re-reads. Its usage is not: the adapter's running total is carried.
        return await terminateRun(step[TERMINATED], stream, live, watchdog, emit, usageSoFar);
      }
      if (step.done) break;
      const message = step.value;
      const cls = classify(message);

      // Streaming frames arrive per token and never reach runtime.log: writing
      // one JSON line per token would turn a diagnostic into the hang it exists
      // to report. Only present under `debug` at all. They still produce a
      // rate-limited heartbeat below, which is bounded by construction.
      if (!(cls.kind === "model_wait" && cls.streaming)) record(message);
      // A message about the server rather than the run (OpenCode's keep-alives,
      // its plugin and catalog announcements). Recorded above and nothing else:
      // translating it would produce nothing, and observing that nothing would
      // reset the watchdog's idle clock — see `MessageClass`.
      if (cls.kind === "noise") continue;
      // A retryable API failure is the answer to "waiting on the model" — see
      // `MessageClass` (runtime/port.ts). It is recorded and reported but NOT
      // passed to observe(): a retry means the run failed to progress, and
      // counting it as activity would suppress the very report it explains.
      // Retries are not translated, so emitting here adds a line rather than
      // duplicating one.
      if (cls.kind === "retry") {
        watchdog.observeRetry(cls.info);
        emit({ kind: "notice", agentId: LEAD_AGENT_ID, level: "warn", code: "api_retry", detail: apiRetryLine(cls.info) });
        continue;
      }
      // The other messages that explain a silence or an ending — a
      // compaction, a refusal, a denied tool, a worker going away, a rate-limit
      // window. Without a notice, a run that was compacting and a run that was
      // wedged look identical. Deliberately NOT fed to the watchdog: none of
      // them is the agent making progress, and firing the idle report slightly
      // early is the safe direction for a diagnostic.
      //
      // A rate-limit sentence the run has already said is NOT this class — the
      // classifier drops the repeat per session (a live run said the same "near
      // the limit on the seven_day window at 83%" seventeen times) and the
      // message falls through to `activity`.
      if (cls.kind === "stall_signal") {
        const { signal } = cls;
        emit({ kind: "notice", agentId: LEAD_AGENT_ID, level: signal.level, code: signal.code, detail: signal.detail });
        continue;
      }
      // "Still waiting" — a streaming frame, a thinking-token delta, or a tool
      // call the runtime says is still running. The adapter turns each into a
      // rate-limited `heartbeat`, and all three go round `observe()` for the
      // same reason a retry does: a heartbeat says nothing has happened yet, so
      // counting one as activity would keep the watchdog quiet through exactly
      // the stall the heartbeat is reporting. Note that this must hold for the
      // ones the rate limiter DROPS too, which is why the routing is by message
      // class rather than by whether an event came back.
      //
      // The INPUT grace is the opposite call on the same frames, and the two
      // were conflated here until a live run paid for it (see the header). They
      // answer different questions. The watchdog asks "is progress happening?",
      // and a heartbeat is not progress — that stays exactly as it was. The
      // grace asks "is this run still alive?", and a heartbeat is positive proof
      // that it is: the runtime emits one only while a turn or a tool of this
      // run is in flight. So a liveness frame disarms the grace, a tool's as
      // much as the model's — a tool the runtime is still ticking is work this
      // run is doing, and closing stdin under it cancels the hook the write at
      // the end of that work would have answered on. Disarming on proof of life
      // cannot hang a run: a lead with nothing left to say ends its turn, and
      // the `turn_end` path below ends input there when nothing is live. The
      // grace exists only for a lead that is never woken at all, and
      // `createRunDeadline` remains the backstop for a run that never ends.
      if (cls.kind === "model_wait" || cls.kind === "tool_progress") {
        disarmInputGrace();
        // Only a model wait says the model is producing; a slow tool says
        // nothing about the model at all.
        if (cls.kind === "model_wait") watchdog.observeStream();
        for (const event of translate(message)) emit(event);
        continue;
      }
      // Everything from here on is activity; `turn_end`, `task_bookkeeping` and
      // `init` each add one step of their own after it (see MessageClass).
      if (cls.kind === "task_bookkeeping") {
        if (cls.started) live.add(cls.started);
        if (cls.ended) live.delete(cls.ended);
      }
      const events = translate(message);
      watchdog.observe(events);
      for (const event of events) {
        // A `turn_ended` goes out where it happened AND is remembered: it is
        // informational, and the run's own ending is a separate event written
        // when the source closes. See the header.
        if (event.kind === "turn_ended") lastTurn = event as TurnEnded;
        emit(event);
      }
      // The input rule (header). A turn ending with nothing live: the run is
      // over. A task settling after a turn ended, leaving nothing live: the lead
      // is normally woken for another turn — give it the grace, and let any
      // message that is not task bookkeeping (the woken lead's own output)
      // disarm it. A turn ending while tasks are live keeps input open.
      if (cls.kind === "turn_end") {
        disarmInputGrace();
        if (live.size === 0) endInput();
      } else if (cls.kind === "task_bookkeeping") {
        if (lastTurn && live.size === 0) armInputGrace();
      } else {
        disarmInputGrace();
      }
      // The runtime reports what it actually resolved; a preload that matched
      // nothing is dropped in silence (see skills_preload_check.ts for the
      // run this cost us). Warn rather than fail: the guidance is missing,
      // not the build, and a run that can still produce something useful
      // should — but it must not look clean while doing it.
      if (cls.kind === "init" && opts.requestedSkills) {
        const { missing } = checkPreload(opts.requestedSkills, cls.resolvedSkills);
        if (missing.length > 0) {
          emit({ kind: "notice", agentId: LEAD_AGENT_ID, level: "warn", detail: preloadWarning(missing) });
        }
      }
    }

    // A stream that closed without ever reporting a turn's outcome. Still a
    // failure, and still settled: v1 emitted no terminal line at all here, and
    // under the recorder that phase 2 adds "the run never settled" is
    // indistinguishable from "the recording was lost". A settle that says it
    // does not know is a different, readable statement.
    if (!lastTurn) {
      const error = "agent stream ended without result";
      emit({ kind: "run_settled", agentId: LEAD_AGENT_ID, outcome: "failure", error, ...usageSoFar() });
      return { exitCode: 1, error };
    }
    // The exit code follows the SETTLE, so the feed and the process cannot give
    // two answers about one run.
    emit({
      kind: "run_settled",
      agentId: LEAD_AGENT_ID,
      outcome: lastTurn.outcome ?? "failure",
      ...(lastTurn.error ? { error: lastTurn.error } : {}),
      ...(lastTurn.usage ? { usage: lastTurn.usage } : {}),
    });
    if (lastTurn.outcome === "success") return { exitCode: 0 };
    return { exitCode: 1, error: lastTurn.error ?? "agent run failed" };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    record({ type: "worker_error", error: msg });
    emit({ kind: "run_settled", agentId: LEAD_AGENT_ID, outcome: "failure", error: msg, ...usageSoFar() });
    return { exitCode: 1, error: msg };
  } finally {
    disarmInputGrace();
  }
}

/**
 * The early ends, as the one promise the loop races the source against.
 *
 * A single arm is passed through rather than wrapped in a `Promise.race` of
 * one, and no arms at all is `undefined` rather than a promise that never
 * settles: a run configured with neither must take exactly the path it took
 * before either existed.
 */
function earliestEnding(deadline?: RunDeadline, terminator?: RunTerminator): Promise<EarlyEnd> | undefined {
  const arms: Promise<EarlyEnd>[] = [];
  if (deadline) arms.push(deadline.expiry.then(() => earlyEnd(deadlineTermination(deadline))));
  if (terminator) arms.push(terminator.requested.then(earlyEnd));
  if (arms.length === 0) return undefined;
  return arms.length === 1 ? arms[0] : Promise.race(arms);
}

/** The deadline's reason, built where the budget's wording lives. */
function deadlineTermination(deadline: RunDeadline): RunTermination {
  const budget = budgetText(deadline.budgetMs);
  return {
    source: "deadline",
    why: `the run hit its ${budget} time budget`,
    error: `run terminated after its ${budget} time budget`,
  };
}

/**
 * End a run that is still being read: say why, stop what is still running,
 * settle once.
 *
 * The ONE place that happens, whatever ended the run. The stop phase is shared
 * with it deliberately — a fatal leaves the same background subagents running
 * that a deadline does, and a second ender that skipped it would leave them
 * working inside a pod that is about to exit.
 */
async function terminateRun(
  reason: RunTermination,
  stream: RunStream,
  live: ReadonlySet<string>,
  watchdog: RunWatchdog,
  emit: (event: RunEventInput) => void,
  usageSoFar: () => { usage?: RunEventUsage },
): Promise<RunResult> {
  const ids = [...live];
  const stopping = ids.length > 0 ? `, stopping ${ids.length} running task(s)` : "";
  // Announced BEFORE the stop, so the reason reaches the pipe even if stopping
  // is what hangs. Same shape as the watchdog's own lines, and at `error`
  // because unlike those this one is the end of the run rather than a report on
  // it.
  emit({
    kind: "notice",
    agentId: LEAD_AGENT_ID,
    level: "error",
    code: "terminated",
    detail: `[${reason.source}] terminated — ${reason.why}${stopping} — ${watchdog.describe()}`,
  });
  // In parallel and failure-tolerant: the tasks are independent, and a stop that
  // errors (the task already gone, the session shutting down) must not cost the
  // settle that follows it. Bounded for the same reason: a stop that never
  // answers would turn the ending into the hang it exists to prevent.
  await withTimeout(
    Promise.all(ids.map((id) => stream.stopTask(id).catch(() => {}))),
    STOP_TASKS_TIMEOUT_MS,
    "stopping the live tasks",
  ).catch(() => {});
  // Read after the stop, so a task's last usage report is counted.
  emit({ kind: "run_settled", agentId: LEAD_AGENT_ID, outcome: "failure", error: reason.error, ...usageSoFar() });
  return { exitCode: 1, error: reason.error };
}

/** A budget as a person set it: whole minutes above a minute, seconds below. */
function budgetText(ms: number): string {
  return ms >= 60_000 ? `${Math.round(ms / 60_000)}m` : `${Math.round(ms / 1000)}s`;
}

/**
 * The feed line for one retry.
 *
 * The loop's, not the runtime's: a classifier says a retry happened and with
 * what numbers (`ApiRetryInfo`), and how that reads is the same sentence
 * whichever runtime retried. Every retry gets a line, not just the late ones:
 * the count is bounded by `maxRetries`, and a single retry only ever happens
 * when something IS wrong, so there is no healthy run for this to add noise to.
 * A threshold would have to be tuned against an error class we do not control.
 */
export function apiRetryLine(info: ApiRetryInfo): string {
  // "no response" is the honest rendering of a null status: a refused
  // connection or a timeout never got one, and printing "HTTP 0" would invent
  // a status the API never returned.
  const where = info.errorStatus === null ? "no response" : `HTTP ${info.errorStatus}`;
  // Backoff delays are sub-minute by construction, so plain seconds reads
  // better here than the run-length format the watchdog uses.
  const next = `${Math.round(info.retryDelayMs / 1000)}s`;
  // A runtime that states no ceiling gets none printed: "retry 3" is the fact,
  // "retry 3/0" would be a bound nobody enforces.
  const attempt = info.maxRetries === null ? `${info.attempt}` : `${info.attempt}/${info.maxRetries}`;
  return `[api] retry ${attempt} after ${info.error} (${where}) — next attempt in ${next}`;
}
