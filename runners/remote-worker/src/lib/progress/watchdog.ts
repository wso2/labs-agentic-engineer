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

// Tells a slow run apart from a wedged one.
//
// A coding run's only sign of life is its progress feed, and a genuinely long
// step is indistinguishable from a hang while that feed is silent — one real
// run went 8m49s between lines inside a `bal tool pull`, and nothing said
// whether it was working or dead. The watchdog periodically reports what the
// run is WAITING ON, which is the diagnosis rather than the symptom:
//
//   - a tool is in flight  → the tool is slow or stuck, and the line names it
//   - a subagent is running → its model turn is the slow half, and the line
//                             names WHICH subagent
//   - neither               → the lead's own model turn is slow or stuck
//
// Those are different faults with different fixes, and the feed could not
// previously distinguish them at all.
//
// **The agent case is now DECLARED.** It used to be read off `emitterId`,
// because v1 had no "an agent started" event at all: a fan-out call produced no
// `tool_use`, so the watchdog never had the call in flight and could not name
// it. Measured on a live run, a subagent that went silent for ten minutes and
// then failed produced four reports, every one saying "no tool in flight" while
// a 22-minute `Agent` call was the thing being waited on. Registering from the
// FIRST LINE an agent produced fixed the naming and left the clock a few
// seconds late. v2 removes the guess entirely: `agent_started` opens an agent
// and `agent_settled` closes it, so the clock starts when the runtime says the
// agent did.
//
// "The model turn is stuck" was the end of the diagnosis and is now the middle
// of it: `observeRetry` and `observeStream` carry the two things that tell those
// stalls apart — an API retry storm, or a generation that is simply long. Both
// are recorded WITHOUT counting as activity, and that is the point. A retry is
// the absence of progress, so letting one reset the idle clock would have kept
// the watchdog quiet through exactly the stall it exists to report: the measured
// backoff climbs through 0.2s → 33.6s, all of it inside the idle window.
//
// It reports at `warn`, never `error`, and never terminates the run: a long
// dependency pull is legitimate, and a watchdog that failed the run on its own
// clock would be a worse bug than the silence it replaces.

import { emit as defaultEmit, LEAD_AGENT_ID, type RunEventInput } from "./emitter.js";
import type { ApiRetryInfo } from "./diagnostics.js";

// Long enough that an ordinary compile or install does not trip it, short
// enough that a multi-minute dead zone turns into several informative lines
// instead of one silent terminal.
export const DEFAULT_IDLE_MS = 120_000;

// How a spawned agent is named in a report. Deliberately NOT the runtime's
// fan-out tool name: v2 says an agent started, not that a tool called `Agent`
// was invoked, and this line is read by a person who has no reason to know
// which runtime produced it.
const AGENT_LABEL = "agent";

interface InFlight {
  tool: string;
  summary: string;
  startedAt: number;
  /** A whole spawned agent, as opposed to a tool call made inside one. */
  agent?: boolean;
  /** For a call made inside a spawned agent: which agent. */
  ownerLabel?: string;
}

export interface RunWatchdogOptions {
  /** Silence tolerated before the first report, and between repeats. */
  idleMs?: number;
  now?: () => number;
  emit?: (event: RunEventInput) => void;
}

export interface RunWatchdog {
  /**
   * Record one SDK message's worth of activity, with the events it produced.
   *
   * A `heartbeat` is deliberately not activity: it says the run is ALIVE, not
   * that anything happened, and one every ten seconds would keep the idle clock
   * permanently reset — the watchdog would then go silent through exactly the
   * stall the heartbeats are describing. Same rule as a retry, for the same
   * reason.
   */
  observe(events: readonly RunEventInput[]): void;
  /**
   * Record a retryable API failure. NOT activity — see the header: a retry is
   * the run failing to make progress, and the idle clock has to keep running
   * through it or the report never fires.
   */
  observeRetry(info: ApiRetryInfo): void;
  /**
   * Record a streaming token frame. NOT activity either, so the watchdog fires
   * on the same schedule whether or not the developer-only streaming option is
   * on — a diagnostic that changes the symptom is no use for diagnosing it.
   */
  observeStream(): void;
  /** Run the idle check once. Production drives this from a timer. */
  check(): void;
  /** One line describing what the run is waiting on, right now. */
  describe(): string;
  /** Begin the periodic check; returns a stop function. */
  start(): () => void;
}

function seconds(ms: number): string {
  return ms < 60_000 ? `${Math.round(ms / 1000)}s` : `${Math.floor(ms / 60_000)}m${Math.round((ms % 60_000) / 1000)}s`;
}

export function createRunWatchdog(opts?: RunWatchdogOptions): RunWatchdog {
  const idleMs = opts?.idleMs ?? DEFAULT_IDLE_MS;
  const now = opts?.now ?? Date.now;
  const emit = opts?.emit ?? defaultEmit;

  // Keyed by an agent's id for a whole spawned agent, and by a tool call's id
  // for one call. The two id spaces never collide — one is the runtime's task
  // id, the other its tool_use id — and keeping them in one map is what lets
  // `describe` prefer the innermost thing that is actually stuck.
  const inFlight = new Map<string, InFlight>();
  // Agents that have already settled, so a late line about a FINISHED agent
  // cannot register a phantom that nothing ever closes. One entry per agent the
  // run ever had, so it needs no cap.
  const settledAgents = new Set<string>();
  // An agent's label, for the line that names which one is stuck. Kept here
  // rather than looked up from `inFlight` because a tool call made INSIDE an
  // agent has to name that agent after the agent itself has been settled.
  const agentLabels = new Map<string, string>();
  let lastActivityAt = now();
  // Tracked separately from lastActivityAt so a repeat fires every idleMs of
  // continued silence rather than only once.
  let lastReportAt = lastActivityAt;
  // The two explanations for a silent model turn. Both are cleared by real
  // activity: a retry that was followed by a successful call is history, and
  // reporting it against a LATER stall would name the wrong cause.
  let lastRetry: { info: ApiRetryInfo; at: number } | undefined;
  let lastStreamAt = 0;

  function oldestInFlight(pick: (call: InFlight) => boolean): InFlight | undefined {
    let oldest: InFlight | undefined;
    for (const call of inFlight.values()) {
      if (!pick(call)) continue;
      if (!oldest || call.startedAt < oldest.startedAt) oldest = call;
    }
    return oldest;
  }

  function runningAgents(): InFlight[] {
    return [...inFlight.values()].filter((c) => c.agent);
  }

  function named(call: InFlight): string {
    return call.summary ? `${call.tool} (${call.summary})` : call.tool;
  }

  /**
   * Why the model turn is silent, when we know.
   *
   * Appended to EVERY branch, not just the nothing-in-flight one: a stall
   * inside a subagent surfaces under that subagent's line, and that is the case
   * where the cause is hardest to guess from outside. Whether an api_retry
   * raised INSIDE a subagent reaches the lead's message stream at all is not
   * established — no retry frame arrived during the live stall this was
   * measured on, which is consistent with both "no retries" and "not
   * forwarded", so an empty cause here claims nothing.
   */
  function cause(t: number): string {
    if (lastRetry) {
      const { attempt, maxRetries, error } = lastRetry.info;
      return ` (API retry ${attempt}/${maxRetries}, ${error}, last ${seconds(t - lastRetry.at)} ago)`;
    }
    // Only ever known when the streaming option is on. Absence is not
    // "no tokens" — it is "not measured" — so nothing is claimed here.
    if (lastStreamAt) return ` (model streaming, last token ${seconds(t - lastStreamAt)} ago)`;
    return "";
  }

  function describe(): string {
    const t = now();
    // A real tool beats the agent that owns it: "waiting on Bash (npm ci)" is
    // the diagnosis, and "waiting on an agent" while a Bash of its own is open
    // would be the symptom one level up.
    const oldest = oldestInFlight((c) => !c.agent);
    if (oldest) {
      const where = oldest.ownerLabel ? ` in agent (${oldest.ownerLabel})` : "";
      return `waiting on ${named(oldest)}${where} for ${seconds(t - oldest.startedAt)}${cause(t)}`;
    }
    const agents = runningAgents();
    if (agents.length === 1) {
      const a = agents[0];
      return (
        `no tool in flight inside ${named(a)}, running ${seconds(t - a.startedAt)}` +
        ` — waiting on its model for ${seconds(t - lastActivityAt)}${cause(t)}`
      );
    }
    if (agents.length > 1) {
      // Naming one of several would be a coin flip: the lines interleave and
      // any of them could be the silent one.
      return (
        `no tool in flight in any of ${agents.length} running agents` +
        ` — waiting on the model for ${seconds(t - lastActivityAt)}${cause(t)}`
      );
    }
    return `no tool in flight — waiting on the model for ${seconds(t - lastActivityAt)}${cause(t)}`;
  }

  return {
    observe(events) {
      // A feed that says only "still alive" is not a feed that says "still
      // working". Heartbeats are stripped BEFORE the clock is touched, and a
      // message that produced nothing but heartbeats leaves the idle window
      // running — which is the whole point of having them.
      const work = events.filter((e) => e.kind !== "heartbeat");
      if (work.length === 0 && events.length > 0) return;
      lastActivityAt = now();
      lastReportAt = lastActivityAt;
      lastRetry = undefined;
      lastStreamAt = 0;
      for (const e of work) {
        // An agent's life is DECLARED in v2 — see the header. The clock starts
        // when the runtime says the agent started, not at its first visible
        // line, and it stops at the settle rather than at a tool_result whose
        // ids happened to line up.
        if (e.kind === "agent_started" && e.agentId) {
          if (e.label) agentLabels.set(e.agentId, e.label);
          if (!settledAgents.has(e.agentId)) {
            inFlight.set(e.agentId, {
              tool: AGENT_LABEL,
              summary: e.label ?? "",
              startedAt: now(),
              agent: true,
            });
          }
          continue;
        }
        if (e.kind === "agent_settled" && e.agentId) {
          settledAgents.add(e.agentId);
          inFlight.delete(e.agentId);
          continue;
        }
        if (!e.toolUseId) continue;
        if (e.kind === "tool_result") {
          inFlight.delete(e.toolUseId);
          continue;
        }
        // Every other kind carrying a call id IS a call going out — including
        // the git_commit/git_push/gh_action rewrites of a Bash command.
        const tool = typeof e.tool === "string" ? e.tool : e.kind;
        const summary = typeof e.summary === "string" ? e.summary : "";
        const ownerLabel = e.agentId && e.agentId !== LEAD_AGENT_ID ? agentLabels.get(e.agentId) : undefined;
        inFlight.set(e.toolUseId, {
          tool,
          summary,
          startedAt: now(),
          // Which agent's work this is, so a stuck tool names its own section
          // rather than leaving the reader to scroll for it.
          ...(ownerLabel ? { ownerLabel } : {}),
        });
      }
    },

    observeRetry(info) {
      lastRetry = { info, at: now() };
    },

    observeStream() {
      lastStreamAt = now();
    },

    check() {
      if (now() - lastReportAt < idleMs) return;
      lastReportAt = now();
      // A code-less notice: the closed `code` set names CONDITIONS a consumer
      // branches on, and "the run has been quiet" is not one of them — it is a
      // sentence for a reader. v2's structured answer to the same question is
      // the heartbeat's `waitingOn`; this line is the diagnosis behind it.
      emit({ kind: "notice", agentId: LEAD_AGENT_ID, level: "warn", detail: `[watchdog] ${describe()}` });
    },

    describe,

    start() {
      // unref'd: the watchdog must never be the reason a finished run's process
      // stays alive — that would turn a diagnostic into the hang it reports on.
      const timer = setInterval(() => this.check(), Math.max(1_000, Math.floor(idleMs / 4)));
      timer.unref?.();
      return () => clearInterval(timer);
    },
  };
}
